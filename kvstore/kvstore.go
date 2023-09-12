package kvstore

import (
	"encoding/base64"
	"fmt"
	"regexp"
	"strings"

	"github.com/peterbourgon/diskv/v3"
)

var (
	globalDiskv *diskv.Diskv
	nsRegex     = regexp.MustCompile("^[a-zA-Z0-9_]+$")
)

var (
	ErrUnavailable  = fmt.Errorf("kvstore is unavailable")
	ErrBadNamespace = fmt.Errorf("bad namespace")
)

func advancedTransform(s string) *diskv.PathKey {
	parts := strings.SplitN(s, "/", 2)
	if len(parts) == 2 {
		return &diskv.PathKey{
			Path:     parts[:1],
			FileName: base64.URLEncoding.EncodeToString([]byte(parts[1])),
		}
	}
	return &diskv.PathKey{
		Path:     []string{},
		FileName: base64.URLEncoding.EncodeToString([]byte(s)),
	}
}

func inverseTransform(pathKey *diskv.PathKey) string {
	data, _ := base64.URLEncoding.DecodeString(pathKey.FileName)
	if len(pathKey.Path) == 0 {
		return string(data)
	}
	return strings.Join(pathKey.Path, "/") + "/" + string(data)
}

func InitKVStore(dir string, cacheSize uint) error {
	d := diskv.New(diskv.Options{
		BasePath:          dir,
		CacheSizeMax:      uint64(cacheSize),
		AdvancedTransform: advancedTransform,
		InverseTransform:  inverseTransform,
	})
	err := d.Write("_internal/init", []byte("inited"))
	if err != nil {
		return err
	}
	globalDiskv = d
	return nil
}

func checkNamespace(ns string) error {
	if !nsRegex.MatchString(ns) {
		return ErrBadNamespace
	}
	return nil
}

func Write(ns string, key string, value string) error {
	if globalDiskv == nil {
		return ErrUnavailable
	}
	if err := checkNamespace(ns); err != nil {
		return err
	}
	return globalDiskv.WriteString(ns+"/"+key, value)
}

func Read(ns string, key string) (string, error) {
	if globalDiskv == nil {
		return "", ErrUnavailable
	}
	data, err := globalDiskv.Read(ns + "/" + key)
	if err != nil {
		return "", err
	}
	return string(data), nil
}
