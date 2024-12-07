package internal

type dbService struct {
	impl map[string]string
}

func newDBService() *dbService {
	db := make(map[string]string)
	return &dbService{
		impl: db,
	}
}

func (db *dbService) eval(t string, key string, value *string) {
	if t == "create" {
		db.impl[key] = *value
	} else if t == "update" {
		db.impl[key] = *value
	} else if t == "delete" {
		delete(db.impl, key)
	}
}

func (db *dbService) get(key string) (string, bool) {
	val, ok := db.impl[key]
	return val, ok
}
