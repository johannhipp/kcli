package state

import "context"

// SchemaTables lists the application table names in the profile database. It is
// deliberately hand-written because it introspects sqlite_schema, which the
// state package's SQL query layer leaves to the caller rather than modelling as
// a business query.
func (d *DB) SchemaTables(ctx context.Context) ([]string, error) {
	rows, err := d.sql.QueryContext(ctx, `SELECT name FROM sqlite_schema WHERE type = 'table' AND name NOT LIKE 'sqlite_%' ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var names []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		names = append(names, name)
	}
	return names, rows.Err()
}
