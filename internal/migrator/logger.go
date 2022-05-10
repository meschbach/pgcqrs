package migrator

import "fmt"

type migratorLogger struct {
}

func (m *migratorLogger) Printf(format string, v ...interface{}) {
	fmt.Printf(format, v...)
}

func (m *migratorLogger) Verbose() bool {
	return true
}
