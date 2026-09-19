package workspace

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/tobilg/neoserver/internal/datasource"
)

// FeatureInfo and the query methods below resolve a publication, not a
// physical table. A SQL view must never fall back to SourceLayer.
func (l *Layer) FeatureInfo(ctx context.Context, ds datasource.DataSource) (*datasource.LayerInfo, error) {
	if !l.IsSQLView {
		return ds.GetLayerInfo(ctx, l.SourceLayer)
	}
	_, config, err := l.sqlViewSource(ds)
	if err != nil {
		return nil, err
	}
	info := &datasource.LayerInfo{Name: l.PublicID, Title: l.Title, GeometryColumn: config.GeometryColumn, GeometryType: config.GeometryType, SRID: config.SRID, IDColumn: config.IDColumn}
	for _, prop := range config.Properties {
		info.Properties = append(info.Properties, datasource.PropertyInfo{Name: prop.Name, Type: prop.Type, JSONType: datasource.JSONType(prop.Type)})
	}
	return info, nil
}

func (l *Layer) QueryFeatures(ctx context.Context, ds datasource.DataSource, params datasource.QueryParams) ([]json.RawMessage, error) {
	if !l.IsSQLView {
		return ds.Query(ctx, l.SourceLayer, params)
	}
	view, config, err := l.sqlViewSource(ds)
	if err != nil {
		return nil, err
	}
	return view.QuerySQLView(ctx, config, params)
}

func (l *Layer) CountFeatures(ctx context.Context, ds datasource.DataSource, params datasource.QueryParams) (int, error) {
	if !l.IsSQLView {
		return ds.Count(ctx, l.SourceLayer, params)
	}
	view, config, err := l.sqlViewSource(ds)
	if err != nil {
		return 0, err
	}
	return view.CountSQLView(ctx, config, params)
}

func (l *Layer) FeatureByID(ctx context.Context, ds datasource.DataSource, id string, outputSRID int) (json.RawMessage, bool, error) {
	if !l.IsSQLView {
		return ds.QueryByID(ctx, l.SourceLayer, id, outputSRID)
	}
	view, config, err := l.sqlViewSource(ds)
	if err != nil {
		return nil, false, err
	}
	if config.IDColumn == "" {
		return nil, false, fmt.Errorf("SQL view %s requires an id_column for item lookup", l.PublicID)
	}
	features, err := view.QuerySQLView(ctx, config, datasource.QueryParams{Limit: 2, OutputSRID: outputSRID, FeatureIDs: []string{id}})
	if err != nil {
		return nil, false, err
	}
	if len(features) > 1 {
		return nil, false, fmt.Errorf("SQL view %s id_column is not unique", l.PublicID)
	}
	if len(features) == 0 {
		return nil, false, nil
	}
	return features[0], true, nil
}

func (l *Layer) sqlViewSource(ds datasource.DataSource) (datasource.SQLViewDataSource, *datasource.SQLViewConfig, error) {
	view, ok := ds.(datasource.SQLViewDataSource)
	if !ok || l.SQLViewConfig == nil {
		return nil, nil, fmt.Errorf("SQL view %s has no usable SQL-view source/configuration", l.PublicID)
	}
	cfg := l.SQLViewConfig
	config := &datasource.SQLViewConfig{SQL: cfg.SQL, GeometryColumn: cfg.GeometryColumn, GeometryType: cfg.GeometryType, SRID: cfg.SRID, IDColumn: cfg.IDColumn, ReadOnly: true}
	for _, prop := range cfg.Properties {
		if prop != nil {
			config.Properties = append(config.Properties, &datasource.SQLViewProperty{Name: prop.Name, Type: prop.Type})
		}
	}
	return view, config, nil
}
