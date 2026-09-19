package wfs

import (
	"strings"

	"github.com/tobilg/neoserver/internal/datasource"
)

// FESCompileOptions contains options for compiling FES to SQL.
type FESCompileOptions struct {
	Dialect            datasource.SQLDialect
	TableAlias         string
	GeometryExpression string
	StartParamIndex    int
	SourceSRID         int
	GeometryProperty   string
	AllowedProperties  map[string]struct{}
	CollectionID       string // The collection ID being queried (for ResourceId validation)
	IDColumn           string // The ID column name in the table (defaults to "id" if empty)
}

// CompileFES compiles an FES filter to parameterized SQL.
func CompileFES(filter *FESFilter, opts FESCompileOptions) (string, []interface{}, int, error) {
	if opts.StartParamIndex <= 0 {
		opts.StartParamIndex = 1
	}
	if opts.SourceSRID == 0 {
		opts.SourceSRID = 4326
	}
	if opts.TableAlias == "" {
		opts.TableAlias = "t"
	}
	idColumn := opts.IDColumn
	if idColumn == "" {
		idColumn = "id" // Default to "id" if not specified
	}
	compiler := &fesCompiler{
		dialect:            opts.Dialect,
		tableAlias:         opts.TableAlias,
		geometryExpression: opts.GeometryExpression,
		paramIdx:           opts.StartParamIndex,
		sourceSRID:         opts.SourceSRID,
		geomProp:           opts.GeometryProperty,
		allowedProperties:  opts.AllowedProperties,
		collectionID:       opts.CollectionID,
		idColumn:           idColumn,
		args:               []interface{}{},
	}

	sql, err := compiler.compile(filter)
	if err != nil {
		return "", nil, 0, err
	}

	return sql, compiler.args, compiler.paramIdx, nil
}

type fesCompiler struct {
	dialect            datasource.SQLDialect
	tableAlias         string
	geometryExpression string
	paramIdx           int
	sourceSRID         int
	geomProp           string
	allowedProperties  map[string]struct{}
	collectionID       string
	idColumn           string
	args               []interface{}
}

func (c *fesCompiler) compile(filter *FESFilter) (string, error) {
	var conditions []string

	// Handle logical operators
	if filter.And != nil {
		sql, err := c.compileAnd(filter.And)
		if err != nil {
			return "", err
		}
		conditions = append(conditions, sql)
	}

	if filter.Or != nil {
		sql, err := c.compileOr(filter.Or)
		if err != nil {
			return "", err
		}
		conditions = append(conditions, sql)
	}

	if filter.Not != nil {
		sql, err := c.compileNot(filter.Not)
		if err != nil {
			return "", err
		}
		conditions = append(conditions, sql)
	}

	// Handle comparison predicates
	if filter.PropertyIsEqualTo != nil {
		sql, err := c.compileComparison(filter.PropertyIsEqualTo, "=")
		if err != nil {
			return "", err
		}
		conditions = append(conditions, sql)
	}

	if filter.PropertyIsNotEqualTo != nil {
		sql, err := c.compileComparison(filter.PropertyIsNotEqualTo, "<>")
		if err != nil {
			return "", err
		}
		conditions = append(conditions, sql)
	}

	if filter.PropertyIsLessThan != nil {
		sql, err := c.compileComparison(filter.PropertyIsLessThan, "<")
		if err != nil {
			return "", err
		}
		conditions = append(conditions, sql)
	}

	if filter.PropertyIsGreaterThan != nil {
		sql, err := c.compileComparison(filter.PropertyIsGreaterThan, ">")
		if err != nil {
			return "", err
		}
		conditions = append(conditions, sql)
	}

	if filter.PropertyIsLessThanOrEqualTo != nil {
		sql, err := c.compileComparison(filter.PropertyIsLessThanOrEqualTo, "<=")
		if err != nil {
			return "", err
		}
		conditions = append(conditions, sql)
	}

	if filter.PropertyIsGreaterThanOrEqualTo != nil {
		sql, err := c.compileComparison(filter.PropertyIsGreaterThanOrEqualTo, ">=")
		if err != nil {
			return "", err
		}
		conditions = append(conditions, sql)
	}

	if filter.PropertyIsLike != nil {
		sql, err := c.compileLike(filter.PropertyIsLike)
		if err != nil {
			return "", err
		}
		conditions = append(conditions, sql)
	}

	if filter.PropertyIsNull != nil {
		sql, err := c.compileIsNull(filter.PropertyIsNull)
		if err != nil {
			return "", err
		}
		conditions = append(conditions, sql)
	}

	if filter.PropertyIsNil != nil {
		sql, err := c.compileIsNil(filter.PropertyIsNil)
		if err != nil {
			return "", err
		}
		conditions = append(conditions, sql)
	}

	if filter.PropertyIsBetween != nil {
		sql, err := c.compileBetween(filter.PropertyIsBetween)
		if err != nil {
			return "", err
		}
		conditions = append(conditions, sql)
	}

	// Handle spatial predicates
	if filter.BBOX != nil {
		sql, err := c.compileBBOX(filter.BBOX)
		if err != nil {
			return "", err
		}
		conditions = append(conditions, sql)
	}

	if filter.Intersects != nil {
		sql, err := c.compileSpatial(filter.Intersects, "ST_Intersects")
		if err != nil {
			return "", err
		}
		conditions = append(conditions, sql)
	}

	if filter.Within != nil {
		sql, err := c.compileSpatial(filter.Within, "ST_Within")
		if err != nil {
			return "", err
		}
		conditions = append(conditions, sql)
	}

	if filter.Contains != nil {
		sql, err := c.compileSpatial(filter.Contains, "ST_Contains")
		if err != nil {
			return "", err
		}
		conditions = append(conditions, sql)
	}

	if filter.Disjoint != nil {
		sql, err := c.compileSpatial(filter.Disjoint, "ST_Disjoint")
		if err != nil {
			return "", err
		}
		conditions = append(conditions, sql)
	}

	if filter.Touches != nil {
		sql, err := c.compileSpatial(filter.Touches, "ST_Touches")
		if err != nil {
			return "", err
		}
		conditions = append(conditions, sql)
	}

	if filter.Crosses != nil {
		sql, err := c.compileSpatial(filter.Crosses, "ST_Crosses")
		if err != nil {
			return "", err
		}
		conditions = append(conditions, sql)
	}

	if filter.Overlaps != nil {
		sql, err := c.compileSpatial(filter.Overlaps, "ST_Overlaps")
		if err != nil {
			return "", err
		}
		conditions = append(conditions, sql)
	}

	if filter.DWithin != nil {
		sql, err := c.compileDWithin(filter.DWithin)
		if err != nil {
			return "", err
		}
		conditions = append(conditions, sql)
	}

	// Handle temporal predicates
	if filter.After != nil {
		sql, err := c.compileAfter(filter.After)
		if err != nil {
			return "", err
		}
		conditions = append(conditions, sql)
	}

	if filter.Before != nil {
		sql, err := c.compileBefore(filter.Before)
		if err != nil {
			return "", err
		}
		conditions = append(conditions, sql)
	}

	if filter.During != nil {
		sql, err := c.compileDuring(filter.During)
		if err != nil {
			return "", err
		}
		conditions = append(conditions, sql)
	}

	if filter.Begins != nil {
		sql, err := c.compileBegins(filter.Begins)
		if err != nil {
			return "", err
		}
		conditions = append(conditions, sql)
	}

	if filter.BegunBy != nil {
		sql, err := c.compileBegunBy(filter.BegunBy)
		if err != nil {
			return "", err
		}
		conditions = append(conditions, sql)
	}

	if filter.TContains != nil {
		sql, err := c.compileTContains(filter.TContains)
		if err != nil {
			return "", err
		}
		conditions = append(conditions, sql)
	}

	if filter.TEquals != nil {
		sql, err := c.compileTEquals(filter.TEquals)
		if err != nil {
			return "", err
		}
		conditions = append(conditions, sql)
	}

	if filter.TOverlaps != nil {
		sql, err := c.compileTOverlaps(filter.TOverlaps)
		if err != nil {
			return "", err
		}
		conditions = append(conditions, sql)
	}

	if filter.Meets != nil {
		sql, err := c.compileMeets(filter.Meets)
		if err != nil {
			return "", err
		}
		conditions = append(conditions, sql)
	}

	if filter.OverlappedBy != nil {
		sql, err := c.compileOverlappedBy(filter.OverlappedBy)
		if err != nil {
			return "", err
		}
		conditions = append(conditions, sql)
	}

	if filter.MetBy != nil {
		sql, err := c.compileMetBy(filter.MetBy)
		if err != nil {
			return "", err
		}
		conditions = append(conditions, sql)
	}

	if filter.Ends != nil {
		sql, err := c.compileEnds(filter.Ends)
		if err != nil {
			return "", err
		}
		conditions = append(conditions, sql)
	}

	if filter.EndedBy != nil {
		sql, err := c.compileEndedBy(filter.EndedBy)
		if err != nil {
			return "", err
		}
		conditions = append(conditions, sql)
	}

	if filter.AnyInteracts != nil {
		sql, err := c.compileAnyInteracts(filter.AnyInteracts)
		if err != nil {
			return "", err
		}
		conditions = append(conditions, sql)
	}

	// Handle resource IDs
	if len(filter.ResourceId) > 0 {
		sql, err := c.compileResourceIds(filter.ResourceId)
		if err != nil {
			return "", err
		}
		conditions = append(conditions, sql)
	}

	if len(conditions) == 0 {
		return "", nil
	}

	return "(" + strings.Join(conditions, " AND ") + ")", nil
}

func (c *fesCompiler) compileAnd(and *FESAnd) (string, error) {
	var parts []string

	// Compile nested And
	for _, nested := range and.And {
		sql, err := c.compileAnd(nested)
		if err != nil {
			return "", err
		}
		if sql != "" {
			parts = append(parts, sql)
		}
	}

	// Compile nested Or
	for _, nested := range and.Or {
		sql, err := c.compileOr(nested)
		if err != nil {
			return "", err
		}
		if sql != "" {
			parts = append(parts, sql)
		}
	}

	// Compile nested Not
	for _, nested := range and.Not {
		sql, err := c.compileNot(nested)
		if err != nil {
			return "", err
		}
		if sql != "" {
			parts = append(parts, sql)
		}
	}

	// Compile comparison predicates
	for _, comp := range and.PropertyIsEqualTo {
		sql, err := c.compileComparison(comp, "=")
		if err != nil {
			return "", err
		}
		parts = append(parts, sql)
	}
	for _, comp := range and.PropertyIsNotEqualTo {
		sql, err := c.compileComparison(comp, "<>")
		if err != nil {
			return "", err
		}
		parts = append(parts, sql)
	}
	for _, comp := range and.PropertyIsLessThan {
		sql, err := c.compileComparison(comp, "<")
		if err != nil {
			return "", err
		}
		parts = append(parts, sql)
	}
	for _, comp := range and.PropertyIsGreaterThan {
		sql, err := c.compileComparison(comp, ">")
		if err != nil {
			return "", err
		}
		parts = append(parts, sql)
	}
	for _, comp := range and.PropertyIsLessThanOrEqualTo {
		sql, err := c.compileComparison(comp, "<=")
		if err != nil {
			return "", err
		}
		parts = append(parts, sql)
	}
	for _, comp := range and.PropertyIsGreaterThanOrEqualTo {
		sql, err := c.compileComparison(comp, ">=")
		if err != nil {
			return "", err
		}
		parts = append(parts, sql)
	}

	// Compile like predicates
	for _, like := range and.PropertyIsLike {
		sql, err := c.compileLike(like)
		if err != nil {
			return "", err
		}
		parts = append(parts, sql)
	}

	// Compile null predicates
	for _, isNull := range and.PropertyIsNull {
		sql, err := c.compileIsNull(isNull)
		if err != nil {
			return "", err
		}
		parts = append(parts, sql)
	}

	// Compile nil predicates
	for _, isNil := range and.PropertyIsNil {
		sql, err := c.compileIsNil(isNil)
		if err != nil {
			return "", err
		}
		parts = append(parts, sql)
	}

	// Compile between predicates
	for _, between := range and.PropertyIsBetween {
		sql, err := c.compileBetween(between)
		if err != nil {
			return "", err
		}
		parts = append(parts, sql)
	}

	// Compile spatial predicates
	for _, bbox := range and.BBOX {
		sql, err := c.compileBBOX(bbox)
		if err != nil {
			return "", err
		}
		parts = append(parts, sql)
	}
	for _, spatial := range and.Intersects {
		sql, err := c.compileSpatial(spatial, "ST_Intersects")
		if err != nil {
			return "", err
		}
		parts = append(parts, sql)
	}
	for _, spatial := range and.Within {
		sql, err := c.compileSpatial(spatial, "ST_Within")
		if err != nil {
			return "", err
		}
		parts = append(parts, sql)
	}
	for _, spatial := range and.Contains {
		sql, err := c.compileSpatial(spatial, "ST_Contains")
		if err != nil {
			return "", err
		}
		parts = append(parts, sql)
	}
	for _, spatial := range and.Disjoint {
		sql, err := c.compileSpatial(spatial, "ST_Disjoint")
		if err != nil {
			return "", err
		}
		parts = append(parts, sql)
	}
	for _, spatial := range and.Touches {
		sql, err := c.compileSpatial(spatial, "ST_Touches")
		if err != nil {
			return "", err
		}
		parts = append(parts, sql)
	}
	for _, spatial := range and.Crosses {
		sql, err := c.compileSpatial(spatial, "ST_Crosses")
		if err != nil {
			return "", err
		}
		parts = append(parts, sql)
	}
	for _, spatial := range and.Overlaps {
		sql, err := c.compileSpatial(spatial, "ST_Overlaps")
		if err != nil {
			return "", err
		}
		parts = append(parts, sql)
	}
	for _, dwithin := range and.DWithin {
		sql, err := c.compileDWithin(dwithin)
		if err != nil {
			return "", err
		}
		parts = append(parts, sql)
	}

	// Compile temporal predicates
	for _, after := range and.After {
		sql, err := c.compileAfter(after)
		if err != nil {
			return "", err
		}
		parts = append(parts, sql)
	}
	for _, before := range and.Before {
		sql, err := c.compileBefore(before)
		if err != nil {
			return "", err
		}
		parts = append(parts, sql)
	}
	for _, during := range and.During {
		sql, err := c.compileDuring(during)
		if err != nil {
			return "", err
		}
		parts = append(parts, sql)
	}
	for _, begins := range and.Begins {
		sql, err := c.compileBegins(begins)
		if err != nil {
			return "", err
		}
		parts = append(parts, sql)
	}
	for _, begunBy := range and.BegunBy {
		sql, err := c.compileBegunBy(begunBy)
		if err != nil {
			return "", err
		}
		parts = append(parts, sql)
	}
	for _, tcontains := range and.TContains {
		sql, err := c.compileTContains(tcontains)
		if err != nil {
			return "", err
		}
		parts = append(parts, sql)
	}
	for _, tequals := range and.TEquals {
		sql, err := c.compileTEquals(tequals)
		if err != nil {
			return "", err
		}
		parts = append(parts, sql)
	}
	for _, toverlaps := range and.TOverlaps {
		sql, err := c.compileTOverlaps(toverlaps)
		if err != nil {
			return "", err
		}
		parts = append(parts, sql)
	}
	for _, meets := range and.Meets {
		sql, err := c.compileMeets(meets)
		if err != nil {
			return "", err
		}
		parts = append(parts, sql)
	}
	for _, overlappedBy := range and.OverlappedBy {
		sql, err := c.compileOverlappedBy(overlappedBy)
		if err != nil {
			return "", err
		}
		parts = append(parts, sql)
	}
	for _, metBy := range and.MetBy {
		sql, err := c.compileMetBy(metBy)
		if err != nil {
			return "", err
		}
		parts = append(parts, sql)
	}
	for _, ends := range and.Ends {
		sql, err := c.compileEnds(ends)
		if err != nil {
			return "", err
		}
		parts = append(parts, sql)
	}
	for _, endedBy := range and.EndedBy {
		sql, err := c.compileEndedBy(endedBy)
		if err != nil {
			return "", err
		}
		parts = append(parts, sql)
	}
	for _, anyInteracts := range and.AnyInteracts {
		sql, err := c.compileAnyInteracts(anyInteracts)
		if err != nil {
			return "", err
		}
		parts = append(parts, sql)
	}

	// Compile resource IDs
	if len(and.ResourceId) > 0 {
		sql, err := c.compileResourceIds(and.ResourceId)
		if err != nil {
			return "", err
		}
		parts = append(parts, sql)
	}

	if len(parts) == 0 {
		return "", nil
	}
	return "(" + strings.Join(parts, " AND ") + ")", nil
}

func (c *fesCompiler) compileOr(or *FESOr) (string, error) {
	var parts []string

	// Compile nested And
	for _, nested := range or.And {
		sql, err := c.compileAnd(nested)
		if err != nil {
			return "", err
		}
		if sql != "" {
			parts = append(parts, sql)
		}
	}

	// Compile nested Or
	for _, nested := range or.Or {
		sql, err := c.compileOr(nested)
		if err != nil {
			return "", err
		}
		if sql != "" {
			parts = append(parts, sql)
		}
	}

	// Compile nested Not
	for _, nested := range or.Not {
		sql, err := c.compileNot(nested)
		if err != nil {
			return "", err
		}
		if sql != "" {
			parts = append(parts, sql)
		}
	}

	// Compile comparison predicates
	for _, comp := range or.PropertyIsEqualTo {
		sql, err := c.compileComparison(comp, "=")
		if err != nil {
			return "", err
		}
		parts = append(parts, sql)
	}
	for _, comp := range or.PropertyIsNotEqualTo {
		sql, err := c.compileComparison(comp, "<>")
		if err != nil {
			return "", err
		}
		parts = append(parts, sql)
	}
	for _, comp := range or.PropertyIsLessThan {
		sql, err := c.compileComparison(comp, "<")
		if err != nil {
			return "", err
		}
		parts = append(parts, sql)
	}
	for _, comp := range or.PropertyIsGreaterThan {
		sql, err := c.compileComparison(comp, ">")
		if err != nil {
			return "", err
		}
		parts = append(parts, sql)
	}
	for _, comp := range or.PropertyIsLessThanOrEqualTo {
		sql, err := c.compileComparison(comp, "<=")
		if err != nil {
			return "", err
		}
		parts = append(parts, sql)
	}
	for _, comp := range or.PropertyIsGreaterThanOrEqualTo {
		sql, err := c.compileComparison(comp, ">=")
		if err != nil {
			return "", err
		}
		parts = append(parts, sql)
	}

	// Compile like predicates
	for _, like := range or.PropertyIsLike {
		sql, err := c.compileLike(like)
		if err != nil {
			return "", err
		}
		parts = append(parts, sql)
	}

	// Compile null predicates
	for _, isNull := range or.PropertyIsNull {
		sql, err := c.compileIsNull(isNull)
		if err != nil {
			return "", err
		}
		parts = append(parts, sql)
	}

	// Compile nil predicates
	for _, isNil := range or.PropertyIsNil {
		sql, err := c.compileIsNil(isNil)
		if err != nil {
			return "", err
		}
		parts = append(parts, sql)
	}

	// Compile between predicates
	for _, between := range or.PropertyIsBetween {
		sql, err := c.compileBetween(between)
		if err != nil {
			return "", err
		}
		parts = append(parts, sql)
	}

	// Compile spatial predicates
	for _, bbox := range or.BBOX {
		sql, err := c.compileBBOX(bbox)
		if err != nil {
			return "", err
		}
		parts = append(parts, sql)
	}
	for _, spatial := range or.Intersects {
		sql, err := c.compileSpatial(spatial, "ST_Intersects")
		if err != nil {
			return "", err
		}
		parts = append(parts, sql)
	}
	for _, spatial := range or.Within {
		sql, err := c.compileSpatial(spatial, "ST_Within")
		if err != nil {
			return "", err
		}
		parts = append(parts, sql)
	}
	for _, spatial := range or.Contains {
		sql, err := c.compileSpatial(spatial, "ST_Contains")
		if err != nil {
			return "", err
		}
		parts = append(parts, sql)
	}
	for _, spatial := range or.Disjoint {
		sql, err := c.compileSpatial(spatial, "ST_Disjoint")
		if err != nil {
			return "", err
		}
		parts = append(parts, sql)
	}
	for _, spatial := range or.Touches {
		sql, err := c.compileSpatial(spatial, "ST_Touches")
		if err != nil {
			return "", err
		}
		parts = append(parts, sql)
	}
	for _, spatial := range or.Crosses {
		sql, err := c.compileSpatial(spatial, "ST_Crosses")
		if err != nil {
			return "", err
		}
		parts = append(parts, sql)
	}
	for _, spatial := range or.Overlaps {
		sql, err := c.compileSpatial(spatial, "ST_Overlaps")
		if err != nil {
			return "", err
		}
		parts = append(parts, sql)
	}
	for _, dwithin := range or.DWithin {
		sql, err := c.compileDWithin(dwithin)
		if err != nil {
			return "", err
		}
		parts = append(parts, sql)
	}

	// Compile temporal predicates
	for _, after := range or.After {
		sql, err := c.compileAfter(after)
		if err != nil {
			return "", err
		}
		parts = append(parts, sql)
	}
	for _, before := range or.Before {
		sql, err := c.compileBefore(before)
		if err != nil {
			return "", err
		}
		parts = append(parts, sql)
	}
	for _, during := range or.During {
		sql, err := c.compileDuring(during)
		if err != nil {
			return "", err
		}
		parts = append(parts, sql)
	}
	for _, begins := range or.Begins {
		sql, err := c.compileBegins(begins)
		if err != nil {
			return "", err
		}
		parts = append(parts, sql)
	}
	for _, begunBy := range or.BegunBy {
		sql, err := c.compileBegunBy(begunBy)
		if err != nil {
			return "", err
		}
		parts = append(parts, sql)
	}
	for _, tcontains := range or.TContains {
		sql, err := c.compileTContains(tcontains)
		if err != nil {
			return "", err
		}
		parts = append(parts, sql)
	}
	for _, tequals := range or.TEquals {
		sql, err := c.compileTEquals(tequals)
		if err != nil {
			return "", err
		}
		parts = append(parts, sql)
	}
	for _, toverlaps := range or.TOverlaps {
		sql, err := c.compileTOverlaps(toverlaps)
		if err != nil {
			return "", err
		}
		parts = append(parts, sql)
	}
	for _, meets := range or.Meets {
		sql, err := c.compileMeets(meets)
		if err != nil {
			return "", err
		}
		parts = append(parts, sql)
	}
	for _, overlappedBy := range or.OverlappedBy {
		sql, err := c.compileOverlappedBy(overlappedBy)
		if err != nil {
			return "", err
		}
		parts = append(parts, sql)
	}
	for _, metBy := range or.MetBy {
		sql, err := c.compileMetBy(metBy)
		if err != nil {
			return "", err
		}
		parts = append(parts, sql)
	}
	for _, ends := range or.Ends {
		sql, err := c.compileEnds(ends)
		if err != nil {
			return "", err
		}
		parts = append(parts, sql)
	}
	for _, endedBy := range or.EndedBy {
		sql, err := c.compileEndedBy(endedBy)
		if err != nil {
			return "", err
		}
		parts = append(parts, sql)
	}
	for _, anyInteracts := range or.AnyInteracts {
		sql, err := c.compileAnyInteracts(anyInteracts)
		if err != nil {
			return "", err
		}
		parts = append(parts, sql)
	}

	// Compile resource IDs
	if len(or.ResourceId) > 0 {
		sql, err := c.compileResourceIds(or.ResourceId)
		if err != nil {
			return "", err
		}
		parts = append(parts, sql)
	}

	if len(parts) == 0 {
		return "", nil
	}
	return "(" + strings.Join(parts, " OR ") + ")", nil
}

func (c *fesCompiler) compileNot(not *FESNot) (string, error) {
	var sql string
	var err error

	// Check each possible predicate type
	if not.And != nil {
		sql, err = c.compileAnd(not.And)
	} else if not.Or != nil {
		sql, err = c.compileOr(not.Or)
	} else if not.Not != nil {
		sql, err = c.compileNot(not.Not)
	} else if not.PropertyIsEqualTo != nil {
		sql, err = c.compileComparison(not.PropertyIsEqualTo, "=")
	} else if not.PropertyIsNotEqualTo != nil {
		sql, err = c.compileComparison(not.PropertyIsNotEqualTo, "<>")
	} else if not.PropertyIsLessThan != nil {
		sql, err = c.compileComparison(not.PropertyIsLessThan, "<")
	} else if not.PropertyIsGreaterThan != nil {
		sql, err = c.compileComparison(not.PropertyIsGreaterThan, ">")
	} else if not.PropertyIsLessThanOrEqualTo != nil {
		sql, err = c.compileComparison(not.PropertyIsLessThanOrEqualTo, "<=")
	} else if not.PropertyIsGreaterThanOrEqualTo != nil {
		sql, err = c.compileComparison(not.PropertyIsGreaterThanOrEqualTo, ">=")
	} else if not.PropertyIsLike != nil {
		sql, err = c.compileLike(not.PropertyIsLike)
	} else if not.PropertyIsNull != nil {
		sql, err = c.compileIsNull(not.PropertyIsNull)
	} else if not.PropertyIsNil != nil {
		sql, err = c.compileIsNil(not.PropertyIsNil)
	} else if not.PropertyIsBetween != nil {
		sql, err = c.compileBetween(not.PropertyIsBetween)
	} else if not.BBOX != nil {
		sql, err = c.compileBBOX(not.BBOX)
	} else if not.Intersects != nil {
		sql, err = c.compileSpatial(not.Intersects, "ST_Intersects")
	} else if not.Within != nil {
		sql, err = c.compileSpatial(not.Within, "ST_Within")
	} else if not.Contains != nil {
		sql, err = c.compileSpatial(not.Contains, "ST_Contains")
	} else if not.Disjoint != nil {
		sql, err = c.compileSpatial(not.Disjoint, "ST_Disjoint")
	} else if not.Touches != nil {
		sql, err = c.compileSpatial(not.Touches, "ST_Touches")
	} else if not.Crosses != nil {
		sql, err = c.compileSpatial(not.Crosses, "ST_Crosses")
	} else if not.Overlaps != nil {
		sql, err = c.compileSpatial(not.Overlaps, "ST_Overlaps")
	} else if not.DWithin != nil {
		sql, err = c.compileDWithin(not.DWithin)
	} else if not.After != nil {
		sql, err = c.compileAfter(not.After)
	} else if not.Before != nil {
		sql, err = c.compileBefore(not.Before)
	} else if not.During != nil {
		sql, err = c.compileDuring(not.During)
	} else if not.Begins != nil {
		sql, err = c.compileBegins(not.Begins)
	} else if not.BegunBy != nil {
		sql, err = c.compileBegunBy(not.BegunBy)
	} else if not.TContains != nil {
		sql, err = c.compileTContains(not.TContains)
	} else if not.TEquals != nil {
		sql, err = c.compileTEquals(not.TEquals)
	} else if not.TOverlaps != nil {
		sql, err = c.compileTOverlaps(not.TOverlaps)
	} else if not.Meets != nil {
		sql, err = c.compileMeets(not.Meets)
	} else if not.OverlappedBy != nil {
		sql, err = c.compileOverlappedBy(not.OverlappedBy)
	} else if not.MetBy != nil {
		sql, err = c.compileMetBy(not.MetBy)
	} else if not.Ends != nil {
		sql, err = c.compileEnds(not.Ends)
	} else if not.EndedBy != nil {
		sql, err = c.compileEndedBy(not.EndedBy)
	} else if not.AnyInteracts != nil {
		sql, err = c.compileAnyInteracts(not.AnyInteracts)
	} else if len(not.ResourceId) > 0 {
		sql, err = c.compileResourceIds(not.ResourceId)
	}

	if err != nil {
		return "", err
	}
	if sql == "" {
		return "", nil
	}
	return "NOT " + sql, nil
}
