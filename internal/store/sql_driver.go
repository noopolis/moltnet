package store

import (
	"strconv"
	"strings"

	_ "modernc.org/sqlite"
)

type sqlDialect string

const (
	dialectSQLite   sqlDialect = "sqlite"
	dialectPostgres sqlDialect = "postgres"
)

func bindQuery(dialect sqlDialect, query string) string {
	if dialect != dialectPostgres {
		return query
	}

	var builder strings.Builder
	index := 1
	for _, char := range query {
		if char == '?' {
			builder.WriteByte('$')
			builder.WriteString(strconv.Itoa(index))
			index++
			continue
		}
		builder.WriteRune(char)
	}

	return builder.String()
}
