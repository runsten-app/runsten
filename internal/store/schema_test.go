package store

import (
	"context"
	"testing"
)

// TestEveryTableIsIsolated checks the migrated schema itself, whatever the store code
// does: every table has row-level security enabled and an account_isolation policy on
// its account_id (on its id for accounts). A migration that forgets one of them fails
// here, before any isolation test is written for the new table.
func TestEveryTableIsIsolated(t *testing.T) {
	s, _ := testStore(t)
	rows, err := s.pool.Query(context.Background(), `
		SELECT c.relname, c.relrowsecurity,
		       coalesce(pg_get_expr(p.polqual, p.polrelid), ''),
		       coalesce(pg_get_expr(p.polwithcheck, p.polrelid), '')
		FROM pg_class c
		JOIN pg_namespace n ON n.oid = c.relnamespace
		LEFT JOIN pg_policy p ON p.polrelid = c.oid AND p.polname = 'account_isolation'
		WHERE n.nspname = current_schema() AND c.relkind IN ('r', 'p')
		  AND c.relname <> 'schema_migrations' -- bookkeeping, not granted to runsten_app
		ORDER BY c.relname`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()

	const byAccount = "(account_id = runsten_current_account())"
	n := 0
	for rows.Next() {
		var table, using, check string
		var rls bool
		if err := rows.Scan(&table, &rls, &using, &check); err != nil {
			t.Fatal(err)
		}
		n++
		wantUsing, wantCheck := byAccount, byAccount
		if table == "accounts" {
			// runsten_app only reads accounts: runsten_create_account inserts them.
			wantUsing, wantCheck = "(id = runsten_current_account())", ""
		}
		if !rls {
			t.Errorf("%s: row-level security is not enabled", table)
		}
		if using != wantUsing || check != wantCheck {
			t.Errorf("%s: account_isolation policy USING %q WITH CHECK %q, want USING %q WITH CHECK %q",
				table, using, check, wantUsing, wantCheck)
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if n == 0 {
		t.Fatal("no table found: the query does not look at the migrated schema")
	}
}
