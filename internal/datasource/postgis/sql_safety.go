package postgis

import (
	"encoding/json"
	"fmt"
	"strings"

	pg_query "github.com/pganalyze/pg_query_go/v6"
)

// Keep this allowlist explicit. A prefix such as st_* also admits extension
// functions with external side effects. Database schemas/functions must still
// be operator-controlled; SQL views are not a sandbox against a database owner.
var sqlViewFunctions = wordSet(`abs ceil ceiling floor round trunc mod power sqrt exp ln log sign
 count sum avg min max bool_and bool_or every array_agg string_agg json_agg jsonb_agg
 lower upper length char_length substring substr trim btrim ltrim rtrim replace concat concat_ws
 nullif greatest least coalesce date_part date_trunc to_char to_date to_timestamp extract
 row_number rank dense_rank lag lead first_value last_value nth_value
 json_build_object jsonb_build_object json_build_array jsonb_build_array json_extract_path_text
 array_length array_position unnest generate_series
 geometrytype st_geometrytype st_srid st_setsrid st_transform st_astext st_asbinary st_asgeojson
 st_geomfromtext st_geomfromwkb st_geomfromgeojson st_makepoint st_makeenvelope st_makeline st_makepolygon
 st_x st_y st_z st_area st_length st_perimeter st_distance st_dwithin st_intersects st_contains st_within
 st_touches st_crosses st_overlaps st_disjoint st_equals st_relate st_isvalid st_isempty st_issimple
 st_buffer st_centroid st_pointonsurface st_envelope st_extent st_collect st_union st_intersection
 st_difference st_symdifference st_simplify st_simplifypreservetopology st_makevalid st_force2d
 st_multi st_collectionextract st_dump st_dumppoints st_npoints st_numgeometries st_geometryn
 st_startpoint st_endpoint st_reverse st_snaptogrid st_translate st_scale st_rotate`)

func wordSet(words string) map[string]bool {
	result := make(map[string]bool)
	for _, word := range strings.Fields(words) {
		result[word] = true
	}
	return result
}

func validateSQLStructure(query string) error {
	if len(query) > 1024*1024 {
		return fmt.Errorf("SQL view exceeds 1 MiB")
	}
	encoded, err := pg_query.ParseToJSON(query)
	if err != nil {
		return fmt.Errorf("invalid SQL view syntax: %w", err)
	}
	var tree struct {
		Statements []struct {
			Statement map[string]any `json:"stmt"`
		} `json:"stmts"`
	}
	if err := json.Unmarshal([]byte(encoded), &tree); err != nil {
		return err
	}
	if len(tree.Statements) != 1 || tree.Statements[0].Statement["SelectStmt"] == nil {
		return fmt.Errorf("SQL view must be a single SELECT statement")
	}
	return inspectSQLNode(tree.Statements[0].Statement)
}

func inspectSQLNode(value any) error {
	switch node := value.(type) {
	case []any:
		for _, child := range node {
			if err := inspectSQLNode(child); err != nil {
				return err
			}
		}
	case map[string]any:
		for kind, child := range node {
			if strings.HasSuffix(kind, "Stmt") && kind != "SelectStmt" {
				return fmt.Errorf("SQL view cannot contain %s", kind)
			}
			switch kind {
			case "intoClause", "lockingClause", "RangeTableSample":
				return fmt.Errorf("SQL view cannot contain %s", kind)
			case "FuncCall":
				call, ok := child.(map[string]any)
				if !ok {
					return fmt.Errorf("invalid function")
				}
				names, _ := call["funcname"].([]any)
				parts := make([]string, 0, len(names))
				for _, name := range names {
					entry, _ := name.(map[string]any)
					str, _ := entry["String"].(map[string]any)
					part, _ := str["sval"].(string)
					parts = append(parts, part)
				}
				if len(parts) == 0 || len(parts) > 2 || (len(parts) == 2 && parts[0] != "pg_catalog" && parts[0] != "public") || !sqlViewFunctions[parts[len(parts)-1]] {
					return fmt.Errorf("SQL view function %q is not allowed", strings.Join(parts, "."))
				}
			}
			if err := inspectSQLNode(child); err != nil {
				return err
			}
		}
	}
	return nil
}
