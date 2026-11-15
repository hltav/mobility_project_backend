// Standalone read-only preflight. Never loads dotenv or application startup.
package main

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"time"

	"mobility_processor/internal/database"
)

func main() {
	if err := verify(); err != nil {
		fmt.Fprintln(os.Stderr, "Test cluster verification failed; command blocked.")
		os.Exit(1)
	}
}

func verify() error {
	if os.Getenv("PGSQL_DATABASE") != "open_transit_test" || os.Getenv("TEST_PG_CLUSTER_ID") == "" {
		return fmt.Errorf("invalid test configuration")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	db, err := database.OpenFromEnv(ctx)
	if err != nil {
		return err
	}
	defer db.Close()
	tx, err := db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return err
	}
	defer tx.Rollback()
	rows, err := tx.QueryContext(ctx, `SELECT current_database(),session_user,current_user,
 r.rolsuper OR r.rolcreatedb OR r.rolcreaterole OR r.rolbypassrls
 OR EXISTS (SELECT 1 FROM pg_roles a WHERE (a.rolsuper OR a.rolcreatedb OR a.rolcreaterole OR a.rolbypassrls)
 AND pg_has_role(session_user,a.oid,'MEMBER')),
 m.cluster_id,m.purpose,
 has_table_privilege(current_user,'test_control.cluster_identity','INSERT,UPDATE,DELETE,TRUNCATE')
 FROM pg_roles r CROSS JOIN test_control.cluster_identity m
 WHERE r.rolname=session_user AND m.singleton=true
 AND NOT EXISTS (SELECT 1 FROM pg_database WHERE datname='open_transit')`)
	if err != nil {
		return err
	}
	defer rows.Close()
	n := 0
	for rows.Next() {
		var name, login, effective, marker, purpose string
		var administrative, writable bool
		if err = rows.Scan(&name, &login, &effective, &administrative, &marker, &purpose, &writable); err != nil {
			return err
		}
		if name != "open_transit_test" || login != os.Getenv("PGSQL_USERNAME") || effective != login || administrative || writable || marker != os.Getenv("TEST_PG_CLUSTER_ID") || purpose != "open-transit-tests" {
			return fmt.Errorf("identity mismatch")
		}
		n++
	}
	if err = rows.Err(); err != nil {
		return err
	}
	if n != 1 {
		return fmt.Errorf("missing unique marker")
	}
	return nil
}
