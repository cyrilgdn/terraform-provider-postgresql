package postgresql

import (
	"database/sql"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"
)

// TestAccPostgresqlGrantPolicy_ConcurrentOwnerElevation reproduces the
// concurrency defect from task #75 / MPM-14653: several
// postgresql_grant_policy-style operations, for DIFFERENT recipient roles
// but sharing the SAME schema owner in one database, run concurrently. Each
// one calls withRolesGranted to temporarily elevate the connected
// non-superuser admin to the owner role, exactly as
// resourcePostgreSQLGrantPolicyCreateOrUpdate does. pgLockRole only protects
// the recipient role's own OID (different per goroutine here, so it never
// serializes anything on its own), while the shared owner role's membership
// churn (GRANT/REVOKE <owner> ... <admin>) is unprotected and races across
// the concurrent transactions. Depending on exact timing/PG version this
// surfaces as either "deadlock detected" (40P01) or "tuple concurrently
// updated/deleted" (XX000) — both are manifestations of the same missing
// synchronization, so this test asserts on "any error", not a specific code.
func TestAccPostgresqlGrantPolicy_ConcurrentOwnerElevation(t *testing.T) {
	skipIfNotAcc(t)
	testAccPreCheck(t)

	config := getTestConfig(t)
	dbSuffix, teardown := setupTestDatabase(t, true, true)
	dbName, _ := getTestDBNames(dbSuffix)

	bootstrapDSN := config.connStr("postgres")

	adminName := fmt.Sprintf("tf_tests_admin_%s", dbSuffix)
	ownerName := fmt.Sprintf("tf_tests_owner_%s", dbSuffix)
	schemaName := fmt.Sprintf("tf_tests_ownedschema_%s", dbSuffix)

	const concurrency = 8
	recipientNames := make([]string, concurrency)
	for i := range recipientNames {
		recipientNames[i] = fmt.Sprintf("tf_tests_recipient_%s_%d", dbSuffix, i)
	}

	// Single cleanup, in the order that avoids "role cannot be dropped
	// because some objects depend on it" (2BP01, same class of bug fixed in
	// task #74): the database (and the schema/grants living inside it) must
	// go before the roles that own or were granted on those objects.
	defer func() {
		teardown()
		for _, r := range recipientNames {
			dbExecute(t, bootstrapDSN, fmt.Sprintf("DROP ROLE IF EXISTS %s", r))
		}
		dbExecute(t, bootstrapDSN, fmt.Sprintf("DROP ROLE IF EXISTS %s", ownerName))
		dbExecute(t, bootstrapDSN, fmt.Sprintf("DROP ROLE IF EXISTS %s", adminName))
	}()

	dbExecute(t, bootstrapDSN, fmt.Sprintf("CREATE ROLE %s LOGIN CREATEROLE PASSWORD 'testpwd'", adminName))
	adminConfig := config
	adminConfig.Username = adminName
	adminConfig.Password = "testpwd"
	adminDSN := adminConfig.connStr("postgres")

	// Since PostgreSQL 16, CREATEROLE can administer only roles for which it
	// has ADMIN OPTION. Creating the owner as the test admin gives it that
	// option without making it a member, which is the permission shape
	// withRolesGranted requires and matches a provider admin managing an owner.
	dbExecute(t, adminDSN, fmt.Sprintf("CREATE ROLE %s", ownerName))

	for _, r := range recipientNames {
		dbExecute(t, bootstrapDSN, fmt.Sprintf("CREATE ROLE %s", r))
	}

	dbExecute(t, config.connStr(dbName), fmt.Sprintf("CREATE SCHEMA %s", schemaName))
	dbExecute(t, config.connStr(dbName), fmt.Sprintf("ALTER SCHEMA %s OWNER TO %s", schemaName, ownerName))

	adminDB, err := sql.Open("postgres", adminConfig.connStr(dbName))
	if err != nil {
		t.Fatalf("could not open connection pool as %s: %v", adminName, err)
	}
	defer closeDB(t, adminDB)
	adminDB.SetMaxOpenConns(concurrency * 2)

	errs := make([]error, concurrency)
	var ready, start, wg sync.WaitGroup
	ready.Add(concurrency)
	start.Add(1)
	wg.Add(concurrency)

	for i := 0; i < concurrency; i++ {
		go func(i int) {
			defer wg.Done()

			txn, err := adminDB.Begin()
			if err != nil {
				errs[i] = fmt.Errorf("begin: %w", err)
				ready.Done()
				return
			}
			defer deferredRollback(txn)

			ready.Done()
			start.Wait()

			// Mirrors resourcePostgreSQLGrantPolicyCreateOrUpdate's first
			// step: lock the recipient role (different OID per goroutine,
			// so this alone never serializes the goroutines against each
			// other).
			if err := pgLockRole(txn, recipientNames[i]); err != nil {
				errs[i] = fmt.Errorf("pgLockRole: %w", err)
				return
			}

			err = withRolesGranted(txn, []string{ownerName}, func() error {
				// Widen the race window so the concurrent goroutines'
				// GRANT/REVOKE statements actually overlap instead of
				// merely queueing one after another.
				time.Sleep(50 * time.Millisecond)
				_, err := txn.Exec(fmt.Sprintf(
					"GRANT usage ON SCHEMA %s TO %s", schemaName, recipientNames[i],
				))
				return err
			})
			if err != nil {
				errs[i] = fmt.Errorf("withRolesGranted: %w", err)
				return
			}

			if err := txn.Commit(); err != nil {
				errs[i] = fmt.Errorf("commit: %w", err)
			}
		}(i)
	}

	// Release every goroutine's transaction at (almost) the same instant.
	ready.Wait()
	start.Done()
	wg.Wait()

	var failures []string
	for i, err := range errs {
		if err != nil {
			failures = append(failures, fmt.Sprintf("recipient %d (%s): %v", i, recipientNames[i], err))
		}
	}
	if len(failures) > 0 {
		t.Fatalf(
			"%d/%d concurrent grant_policy-style operations failed sharing owner role %s:\n%s",
			len(failures), concurrency, ownerName, strings.Join(failures, "\n"),
		)
	}

	// Correctness, not just absence of error: every recipient must have
	// actually received the grant, and no temporary elevation may remain.
	checkTxn, err := startTransaction(testAccProvider.Meta().(*Client), dbName)
	if err != nil {
		t.Fatalf("could not start check transaction: %v", err)
	}
	defer deferredRollback(checkTxn)

	for _, r := range recipientNames {
		var hasUsage bool
		if err := checkTxn.QueryRow(
			"SELECT has_schema_privilege($1, $2, 'USAGE')", r, schemaName,
		).Scan(&hasUsage); err != nil {
			t.Fatalf("could not check schema privilege for %s: %v", r, err)
		}
		if !hasUsage {
			t.Errorf("recipient %s never received USAGE on %s", r, schemaName)
		}
	}

	adminElevated, err := isMemberOfRole(checkTxn, ownerName, adminName)
	if err != nil {
		t.Fatalf("could not check admin elevation: %v", err)
	}
	if adminElevated {
		t.Errorf("admin %s is still a member of owner %s after all operations committed (dangling elevation)", adminName, ownerName)
	}
}

// TestAccPostgresqlGrantPolicy_OwnerElevationRollbackLeavesNoMembership
// verifies that when the wrapped function passed to withRolesGranted fails,
// rolling back the enclosing transaction (the standard
// defer deferredRollback(txn) pattern every caller uses) leaves no residual
// admin<->owner membership behind — the temporary elevation must not survive
// outside the transaction it happened in.
func TestAccPostgresqlGrantPolicy_OwnerElevationRollbackLeavesNoMembership(t *testing.T) {
	skipIfNotAcc(t)
	testAccPreCheck(t)

	config := getTestConfig(t)
	dbSuffix, teardown := setupTestDatabase(t, true, true)
	defer teardown()
	dbName, _ := getTestDBNames(dbSuffix)

	bootstrapDSN := config.connStr("postgres")
	adminName := fmt.Sprintf("tf_tests_admin_%s", dbSuffix)
	ownerName := fmt.Sprintf("tf_tests_owner_%s", dbSuffix)

	dbExecute(t, bootstrapDSN, fmt.Sprintf("CREATE ROLE %s LOGIN CREATEROLE PASSWORD 'testpwd'", adminName))
	defer dbExecute(t, bootstrapDSN, fmt.Sprintf("DROP ROLE IF EXISTS %s", adminName))

	adminConfig := config
	adminConfig.Username = adminName
	adminConfig.Password = "testpwd"
	dbExecute(t, adminConfig.connStr("postgres"), fmt.Sprintf("CREATE ROLE %s", ownerName))
	defer dbExecute(t, bootstrapDSN, fmt.Sprintf("DROP ROLE IF EXISTS %s", ownerName))

	adminDB, err := sql.Open("postgres", adminConfig.connStr(dbName))
	if err != nil {
		t.Fatalf("could not open connection pool as %s: %v", adminName, err)
	}
	defer closeDB(t, adminDB)

	txn, err := adminDB.Begin()
	if err != nil {
		t.Fatalf("could not begin transaction: %v", err)
	}

	wrappedErr := fmt.Errorf("forced failure inside withRolesGranted")
	err = withRolesGranted(txn, []string{ownerName}, func() error {
		return wrappedErr
	})
	if err != wrappedErr {
		txn.Rollback()
		t.Fatalf("expected withRolesGranted to propagate the wrapped error, got: %v", err)
	}

	if err := txn.Rollback(); err != nil {
		t.Fatalf("could not rollback: %v", err)
	}

	checkTxn, err := startTransaction(testAccProvider.Meta().(*Client), dbName)
	if err != nil {
		t.Fatalf("could not start check transaction: %v", err)
	}
	defer deferredRollback(checkTxn)

	elevated, err := isMemberOfRole(checkTxn, ownerName, adminName)
	if err != nil {
		t.Fatalf("could not check membership: %v", err)
	}
	if elevated {
		t.Errorf("admin %s is still a member of owner %s after rollback", adminName, ownerName)
	}
}

// TestAccPostgresqlGrantPolicy_OwnerElevationNestedAndIndependentOwners
// covers two remaining safety properties from task #75:
//   - a nested/repeated withRolesGranted call for the SAME owner role, on
//     the SAME transaction, must not self-deadlock (guarded by a hard
//     timeout instead of trusting it will always return);
//   - two DIFFERENT owner roles, elevated concurrently by two independent
//     transactions, must both succeed — locking one owner must not
//     serialize operations against an unrelated owner.
func TestAccPostgresqlGrantPolicy_OwnerElevationNestedAndIndependentOwners(t *testing.T) {
	skipIfNotAcc(t)
	testAccPreCheck(t)

	config := getTestConfig(t)
	dbSuffix, teardown := setupTestDatabase(t, true, true)
	defer teardown()
	dbName, _ := getTestDBNames(dbSuffix)

	bootstrapDSN := config.connStr("postgres")
	adminName := fmt.Sprintf("tf_tests_admin_%s", dbSuffix)
	ownerAName := fmt.Sprintf("tf_tests_ownera_%s", dbSuffix)
	ownerBName := fmt.Sprintf("tf_tests_ownerb_%s", dbSuffix)

	dbExecute(t, bootstrapDSN, fmt.Sprintf("CREATE ROLE %s LOGIN CREATEROLE PASSWORD 'testpwd'", adminName))
	defer dbExecute(t, bootstrapDSN, fmt.Sprintf("DROP ROLE IF EXISTS %s", adminName))

	adminConfig := config
	adminConfig.Username = adminName
	adminConfig.Password = "testpwd"
	adminDSN := adminConfig.connStr("postgres")
	dbExecute(t, adminDSN, fmt.Sprintf("CREATE ROLE %s", ownerAName))
	defer dbExecute(t, bootstrapDSN, fmt.Sprintf("DROP ROLE IF EXISTS %s", ownerAName))
	dbExecute(t, adminDSN, fmt.Sprintf("CREATE ROLE %s", ownerBName))
	defer dbExecute(t, bootstrapDSN, fmt.Sprintf("DROP ROLE IF EXISTS %s", ownerBName))

	adminDB, err := sql.Open("postgres", adminConfig.connStr(dbName))
	if err != nil {
		t.Fatalf("could not open connection pool as %s: %v", adminName, err)
	}
	defer closeDB(t, adminDB)
	adminDB.SetMaxOpenConns(4)

	t.Run("nested_same_owner_no_self_deadlock", func(t *testing.T) {
		txn, err := adminDB.Begin()
		if err != nil {
			t.Fatalf("could not begin transaction: %v", err)
		}
		defer deferredRollback(txn)

		done := make(chan error, 1)
		go func() {
			done <- withRolesGranted(txn, []string{ownerAName}, func() error {
				return withRolesGranted(txn, []string{ownerAName}, func() error {
					return nil
				})
			})
		}()

		select {
		case err := <-done:
			if err != nil {
				t.Fatalf("nested withRolesGranted for the same owner returned an error: %v", err)
			}
		case <-time.After(10 * time.Second):
			t.Fatal("nested withRolesGranted for the same owner role self-deadlocked (timed out)")
		}

		if err := txn.Commit(); err != nil {
			t.Fatalf("could not commit: %v", err)
		}
	})

	t.Run("independent_owners_run_concurrently", func(t *testing.T) {
		const perOwnerDelay = 300 * time.Millisecond

		run := func(owner string) <-chan error {
			out := make(chan error, 1)
			go func() {
				txn, err := adminDB.Begin()
				if err != nil {
					out <- fmt.Errorf("begin: %w", err)
					return
				}
				defer deferredRollback(txn)

				err = withRolesGranted(txn, []string{owner}, func() error {
					time.Sleep(perOwnerDelay)
					return nil
				})
				if err != nil {
					out <- err
					return
				}
				out <- txn.Commit()
			}()
			return out
		}

		start := time.Now()
		errA := run(ownerAName)
		errB := run(ownerBName)

		if err := <-errA; err != nil {
			t.Errorf("elevating owner A failed: %v", err)
		}
		if err := <-errB; err != nil {
			t.Errorf("elevating owner B failed: %v", err)
		}

		// Advisory, generous margin: two independent owners fully
		// serialized would take roughly 2x perOwnerDelay. This is not the
		// primary correctness gate (that's the error checks above), but a
		// regression signal if a future change accidentally re-introduces
		// a lock keyed on something shared across unrelated owners.
		if elapsed := time.Since(start); elapsed > perOwnerDelay*3/2 {
			t.Errorf("elevating two independent owner roles took %s, expected close to %s if truly concurrent", elapsed, perOwnerDelay)
		}
	})
}
