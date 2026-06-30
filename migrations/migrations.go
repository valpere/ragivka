package migrations

import "embed"

// FS embeds all SQL migration files for use with goose.SetBaseFS.
//
//go:embed *.sql
var FS embed.FS
