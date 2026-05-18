package dto

// Model represents a semantic model.
type Model struct {
	Name          string            `json:"name"`
	RefSql        string            `json:"refSql,omitempty"`
	BaseObject    string            `json:"baseObject,omitempty"`
	TableReference *TableReference  `json:"tableReference,omitempty"`
	Columns       []Column          `json:"columns"`
	PrimaryKey    string            `json:"primaryKey,omitempty"`
	Cached        bool              `json:"cached"`
	RefreshTime   string            `json:"refreshTime,omitempty"`
	Properties    map[string]string `json:"properties,omitempty"`
}

func (m Model) IsCached() bool    { return m.Cached }
func (m Model) GetColumns() []Column { return m.Columns }
func (m Model) GetBaseObject() string {
	if m.BaseObject != "" {
		return m.BaseObject
	}
	if m.RefSql != "" {
		return m.Name
	}
	return ""
}
