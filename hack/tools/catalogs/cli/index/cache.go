package index

import (
	bolt "go.etcd.io/bbolt"
	"io"
)

type CatalogIndex struct {
	DB *bolt.DB
}

func (d *CatalogIndex) ListCatalogs() ([]string, error) {
	return nil, nil
}

func (d *CatalogIndex) Update(catalogName, resolvedRef string, source io.Reader) error {
	return nil
}

func (d *CatalogIndex) Remove(catalogName string) error {
	return nil
}

func (d *CatalogIndex) Search() error {
	return nil
}
