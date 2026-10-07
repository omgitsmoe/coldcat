package base

type CatalogSummary struct {
	Catalog      CatalogState
	DiskCount    int64
	FileCount    int64
	ContentCount int64
}
