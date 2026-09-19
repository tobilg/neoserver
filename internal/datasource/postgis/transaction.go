package postgis

import (
	"context"
	"fmt"

	"github.com/tobilg/neoserver/internal/datasource"
)

func geometryExpression(parameter, inputSRID, targetSRID int) string {
	if inputSRID == 0 {
		inputSRID = targetSRID
	}
	// The GML declares its normalized source CRS. The optional SRID is a default
	// for legacy direct callers, never a replacement for coordinate conversion.
	return fmt.Sprintf("ST_Transform(ST_GeomFromGML($%d, %d), %d)", parameter, inputSRID, targetSRID)
}

func mutationIDs(ctx context.Context, exec writeExecutor, info *datasource.LayerInfo, statement string, args []interface{}) ([]string, error) {
	if info.IDColumn == "" {
		return nil, fmt.Errorf("transactional mutations require a stable ID column")
	}
	rows, err := exec.Query(ctx, statement+" RETURNING "+quoteIdent(info.IDColumn)+"::text", args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

func (w *transactionWriter) GetLayerInfo(ctx context.Context, layer string) (*datasource.LayerInfo, error) {
	return w.ds.getLayerInfo(ctx, w.tx, layer)
}
func (w *transactionWriter) UpdateReturning(ctx context.Context, layer string, properties map[string]interface{}, filter string, args []interface{}) ([]string, error) {
	return w.ds.update(ctx, w.tx, layer, properties, filter, args)
}
func (w *transactionWriter) DeleteReturning(ctx context.Context, layer, filter string, args []interface{}) ([]string, error) {
	return w.ds.delete(ctx, w.tx, layer, filter, args)
}

var _ datasource.ReturningFeatureWriter = (*transactionWriter)(nil)
