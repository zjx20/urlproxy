package storage

import (
	"text/template"

	"github.com/zjx20/urlproxy/kvstore"
)

func Funcs() template.FuncMap {
	return template.FuncMap{
		"save":     save,
		"mustSave": mustSave,
		"load":     load,
		"mustLoad": mustLoad,
	}
}

func save(ns string, key string, value string) bool {
	ret, _ := mustSave(ns, key, value)
	return ret
}

func mustSave(ns string, key string, value string) (bool, error) {
	err := kvstore.Write(ns, key, value)
	return err == nil, err
}

func load(ns string, key string) string {
	value, _ := mustLoad(ns, key)
	return value
}

func mustLoad(ns string, key string) (string, error) {
	return kvstore.Read(ns, key)
}
