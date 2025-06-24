package postgresql

import (
	"bytes"
	"database/sql"
	"fmt"
	"log"
	"strconv"
	"strings"

	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/validation"
	"github.com/lib/pq"
)

const (
	taskNameAttr     = "name"
	taskDatabaseAttr = "database"
	taskSchemaAttr   = "schema"
	taskScheduleAttr = "schedule"
	taskQueryAttr    = "query"
)

func resourcePostgreSQLTask() *schema.Resource {
	return &schema.Resource{
		Create: PGResourceFunc(resourcePostgreSQLTaskCreate),
		Read:   PGResourceFunc(resourcePostgreSQLTaskRead),
		Update: PGResourceFunc(resourcePostgreSQLTaskUpdate),
		Delete: PGResourceFunc(resourcePostgreSQLTaskDelete),
		Exists: PGResourceExistsFunc(resourcePostgreSQLTaskExists),
		Importer: &schema.ResourceImporter{
			StateContext: schema.ImportStatePassthroughContext,
		},

		Schema: map[string]*schema.Schema{
			taskDatabaseAttr: {
				Type:        schema.TypeString,
				Optional:    true,
				Computed:    true,
				ForceNew:    true,
				Description: "Used in namespacing the task, to allow for the same task to be run for different databases/schemas. In reality, the task does not exist within a database/schema.",

				DiffSuppressFunc: defaultDiffSuppressFunc,
			},
			taskSchemaAttr: {
				Type:        schema.TypeString,
				Optional:    true,
				Computed:    true,
				ForceNew:    true,
				Description: "Used in namespacing the task, to allow for the same task to be run for different databases/schemas. In reality, the task does not exist within a database/schema.",

				DiffSuppressFunc: defaultDiffSuppressFunc,
			},
			taskNameAttr: {
				Type:        schema.TypeString,
				Required:    true,
				ForceNew:    true,
				Description: "The name of the task.",
			},
			taskQueryAttr: {
				Type:        schema.TypeString,
				Required:    true,
				Description: "The query run by the task.",
			},
			taskScheduleAttr: {
				Type:        schema.TypeString,
				Required:    true,
				ForceNew:    true,
				Description: "The cron schedule on which to run the task.",
			},
		},
	}
}

func resourcePostgreSQLTaskCreate(db *DBConnection, d *schema.ResourceData) error {
	if !db.featureSupported(featureTask) {
		return fmt.Errorf(
			"postgresql_task resource is not supported for this Postgres version (%s)",
			db.version,
		)
	}

	if err := createView(db, d); err != nil {
		return err
	}

	if err := resourcePostgreSQLTaskReadImpl(db, d); err != nil {
		return err
	}

	d.Set(internalTFParsedQueryAttr, d.Get(internalPGParsedQueryAttr).(string))
	return nil
}

func resourcePostgreSQLTaskRead(db *DBConnection, d *schema.ResourceData) error {
	if !db.featureSupported(featureTask) {
		return fmt.Errorf(
			"postgresql_task resource is not supported for this Postgres version (%s)",
			db.version,
		)
	}

	err := resourcePostgreSQLTaskReadImpl(db, d)
	if err != nil {
		return err
	}

	// Detect if the query has been modified in Postgres without Terraform's awareness.
	// For this kind of error, the users must consolidate manually.
	tfQuery := d.Get(internalTFParsedQueryAttr).(string)
	pgQuery := d.Get(internalPGParsedQueryAttr).(string)
	if tfQuery != pgQuery {
		return fmt.Errorf("the task: '%s' has been modified in Postgres", d.Get(taskNameAttr).(string))
	}
	return nil
}

func resourcePostgreSQLTaskUpdate(db *DBConnection, d *schema.ResourceData) error {
	if !db.featureSupported(featureTask) {
		return fmt.Errorf(
			"postgresql_task resource is not supported for this Postgres version (%s)",
			db.version,
		)
	}

	if err := createView(db, d); err != nil {
		return err
	}

	if err := resourcePostgreSQLTaskReadImpl(db, d); err != nil {
		return err
	}

	d.Set(internalTFParsedQueryAttr, d.Get(internalPGParsedQueryAttr).(string))
	return nil
}

func resourcePostgreSQLTaskDelete(db *DBConnection, d *schema.ResourceData) error {
	if !db.featureSupported(featureTask) {
		return fmt.Errorf(
			"postgresql_task resource is not supported for this Postgres version (%s)",
			db.version,
		)
	}

	// Drop task command
	sql, err = genDropTaskCommand(db, d)
	if err != nil {
		return err
	}

	txn, err := startTransaction(db.client)
	if err != nil {
		return err
	}
	defer deferredRollback(txn)

	if _, err := txn.Exec(sql); err != nil {
		return err
	}

	if err := txn.Commit(); err != nil {
		return err
	}

	d.SetId("")

	return nil
}

func resourcePostgreSQLTaskExists(db *DBConnection, d *schema.ResourceData) (bool, error) {
	if !db.featureSupported(featureTask) {
		return false, fmt.Errorf(
			"postgresql_task resource is not supported for this Postgres version (%s)",
			db.version,
		)
	}

	taskID := d.Id()
	if taskID == "" {
		genTaskID, err := genTaskID(db, d)
		if err != nil {
			return err
		}
		taskID = genTaskID
	}
	var taskExists bool

	txn, err := startTransaction(db.client)
	if err != nil {
		return false, err
	}
	defer deferredRollback(txn)

	query := fmt.Sprintf("SELECT count(id) > 0 AS taskExists from cron.job WHERE jobname = $1", taskID)

	if err := txn.QueryRow(query).Scan(&taskExists); err != nil {
		return false, err
	}

	if err := txn.Commit(); err != nil {
		return false, err
	}

	return taskExists, nil
}

type PGTask struct {
	Database string
	Schema   string
	Name     string
	Query    string
	Schedule string
}

type TaskInfo struct {
	Database string `db:"database"`
	Name     string `db:"name"`
	Query    string `db:"query"`
	Schedule string `db:"schedule"`
}

func resourcePostgreSQLTaskReadImpl(db *DBConnection, d *schema.ResourceData) error {
	taskID := d.Id()
	if taskID == "" {
		genTaskID, err := genTaskID(db, d)
		if err != nil {
			return err
		}
		taskID = genTaskID
	}

	query := `SELECT j.database AS database, ` +
		`j.jobname AS name, ` +
		`j.command AS query, ` +
		`j.schedule AS schedule ` +
		`FROM cron.job j ` +
		`WHERE jobname = $1`
	txn, err := startTransaction(db.client)
	if err != nil {
		return err
	}
	defer deferredRollback(txn)

	var taskInfo TaskInfo
	err = txn.QueryRow(query, taskId).Scan(&taskInfo.Database, &taskInfo.Name, &taskInfo.Query, &taskInfo.Schedule)
	switch {
	case err == sql.ErrNoRows:
		log.Printf("[WARN] PostgreSQL task: %s", taskID)
		d.SetId("")
		return nil
	case err != nil:
		return fmt.Errorf("error reading task: %w", err)
	}

	if err := txn.Commit(); err != nil {
		return err
	}

	pgTask, err := parseTask(taskInfo)
	if err != nil {
		return err
	}

	d.Set(taskDatabaseAttr, pgTask.Database)
	d.Set(taskSchemaAttr, pgTask.Schema)
	d.Set(taskNameAttr, pgTask.Name)
	d.Set(taskQueryAttr, pgTask.Query)
	d.Set(taskScheduleAttr, pgTask.Schedule)

	d.SetId(taskID)

	return nil
}

func parseTask(taskInfo TaskInfo) (PGTask, error) {
	var pgTask PGTask
	taskIDParts := strings.Split(taskInfo.Name, ".")
	pgTask.Database = taskIDParts[0]
	pgTask.Schema = taskIDParts[1]
	pgTask.Name = taskIDParts[2]
	pgTask.Query = taskInfo.Query
	pgTask.Schedule = taskInfo.Schedule

	return pgTask, nil
}

func genDropTaskCommand(db *DBConnection, d *schema.ResourceData) (string, error) {
	fullTaskName, err := genTaskID(db, d)
	if err != nil {
		return nil, err
	}
	dropTaskSqlBuffer := bytes.NewBufferString("SELECT cron.unschedule('")
	dropTaskSqlBuffer.WriteString(pq.QuoteLiteral(fullTaskName))
	dropTaskSqlBuffer.WriteString("');")
	dropTaskSql := dropTaskSqlBuffer.String()
	return dropTaskSql, nil
}

func getDatabaseName(db *DBConnection, d *schema.ResourceData) (string, error) {
	if databaseAttr, ok := d.GetOk(taskDatabaseAttr); ok {
		return databaseAttr.(string), nil
	} else {
		return db.client.databaseName, nil
	}
}

func genTaskID(db *DBConnection, d *schema.ResourceData) (string, error) {
	// Generate with format: <database_name>.<schema_name>.<task_name>
	b := bytes.NewBufferString("")
	fmt.Fprint(b, getDatabaseName(db, d), ".")

	schemaName := "public"
	if v, ok := d.GetOk(taskSchemaAttr); ok {
		schemaName = v.(string)
	}
	taskName := d.Get(taskNameAttr).(string)

	fmt.Fprint(b, schemaName, ".", taskName)
	return b.String(), nil
}

func createTask(db *DBConnection, d *schema.ResourceData) error {
	fullTaskName, err := genTaskID(db, d)
	if err != nil {
		return err
	}
	databaseName, err := getDatabaseName(db, d)
	if err != nil {
		return err
	}
	query := d.Get(taskQueryAttr).(string)
	cronSchedule := d.Get(taskScheduleAttr).(string)

	// Construct the task
	b := bytes.NewBufferString("SELECT cron.schedule(")
	fmt.Fprint(b, "'", pq.QuoteLiteral(fullTaskName), "','", pq.QuoteLiteral(cronSchedule), "','", pq.QuoteLiteral(query), "');")
	fmt.Fprint(b, "UPDATE cron.job SET database = '", pq.QuoteLiteral(databaseName), "' WHERE jobname = '", pq.QuoteLiteral(fullTaskName), "' AND database != '", pq.QuoteLiteral(databaseName), "';")

	// Drop task command
	dropTaskSql, err = genDropTaskCommand(db, d)
	if err != nil {
		return err
	}

	sql := b.String()
	txn, err := startTransaction(db.client)
	if err != nil {
		return err
	}
	defer deferredRollback(txn)

	// Drop task if exists
	if _, err := txn.Exec(dropTaskSql); err != nil {
		return err
	}

	if _, err := txn.Exec(sql); err != nil {
		return err
	}

	if err := txn.Commit(); err != nil {
		return err
	}

	return nil
}

func runChecks(db *DBConnection) error {
	if !db.featureSupported(featureTask) {
		return fmt.Errorf(
			"postgresql_task resource is not supported for this Postgres version (%s)",
			db.version,
		)
	}

	var extensionExists bool
	txn, err := startTransaction(db.client)
	if err != nil {
		return err
	}
	defer deferredRollback(txn)

	if err := txn.QueryRow("SELECT count(*) > 0 AS extensionExists FROM pg_extension WHERE extname = 'pg_cron'").Scan(&extensionExists); err != nil {
		return err
	}

	if err := txn.Commit(); err != nil {
		return err
	}

	if !taskExists {
		return fmt.Errorf(
			"The pg_cron extension must be installed on the database before a task is created. Please use the postgresql_extension resource to set it up.",
		)
	}

}
