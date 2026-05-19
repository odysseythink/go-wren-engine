package analyzer

// CatalogSchemaTableName is a fully qualified table name (catalog.schema.table).
// Mirrors Java io.wren.base.CatalogSchemaTableName.
type CatalogSchemaTableName struct {
	Catalog string
	Schema  string
	Table   string
}
