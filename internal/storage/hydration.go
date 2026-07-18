package storage

type sqlScanner interface {
	Scan(dest ...any) error
}
