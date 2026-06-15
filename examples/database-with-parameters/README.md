# Database with Configuration Parameters Example

This example demonstrates how to create a PostgreSQL database with custom configuration parameters using the `parameter` block.

## What This Example Does

This example shows how to:
- Create a database with multiple configuration parameters
- Set numeric parameters (like `work_mem`, `max_parallel_workers`)
- Set string parameters with proper quoting
- Configure database-level settings that override instance defaults

## Configuration Parameters

The example sets the following parameters:

- `work_mem`: Amount of memory to be used by internal sort operations and hash tables before writing to temporary disk files (use `quote = true` for values with units)
- `max_parallel_workers`: Maximum number of parallel workers that can be active at one time (use `quote = false` for numeric values)
- `statement_timeout`: Maximum allowed duration of any statement in milliseconds (use `quote = false` for numeric values)
- `default_statistics_target`: Default statistics target for table columns (use `quote = false` for numeric values)
- `search_path`: Schema search path for unqualified object names (use `quote = true` for string values)

### Quote Parameter Guidelines

- **`quote = true`** (default): Use for:
  - Values with units (e.g., `"16MB"`, `"5min"`)
  - String values (e.g., `"public,app_schema"`)
  - Complex values that need proper escaping

- **`quote = false`**: Use for:
  - Numeric values without units (e.g., `4`, `100`, `30000`)
  - Boolean-like numeric values (e.g., `0`, `1`)

## Usage

1. Configure your PostgreSQL provider connection details in `main.tf`
2. Initialize Terraform:
   ```bash
   terraform init
   ```
3. Plan the changes:
   ```bash
   terraform plan
   ```
4. Apply the configuration:
   ```bash
   terraform apply
   ```

## Verifying the Configuration

After applying, you can verify the database parameters are set correctly:

```sql
-- Connect to the database
\c myapp_db

-- Show all database-specific parameters
SELECT name, setting 
FROM pg_db_role_setting s
JOIN pg_database d ON d.oid = s.setdatabase
WHERE d.datname = 'myapp_db';

-- Or check a specific parameter
SHOW work_mem;
```

## Notes

- The `quote` parameter controls whether the value should be quoted as a string literal
- Set `quote = false` for numeric values and identifiers
- Set `quote = true` (default) for string values
- Parameters set at the database level override server-level defaults for connections to that database

