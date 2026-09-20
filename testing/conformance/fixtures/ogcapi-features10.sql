-- Preserve all published types and their geometry dimensionality. Populate
-- every fixed ETS bbox, without changing the fixtures of any other suite.
DO $$
DECLARE
    layer record;
    target record;
    original geometry;
    sample geometry;
    factor double precision;
    columns_sql text;
    values_sql text;
    layer_count integer := 0;
BEGIN
    FOR layer IN SELECT f_table_schema AS schema_name, f_table_name AS table_name
                 FROM geometry_columns WHERE f_table_schema IN ('public', 'cite')
    LOOP
        layer_count := layer_count + 1;
        EXECUTE format('SELECT geom FROM %I.%I WHERE geom IS NOT NULL ORDER BY id LIMIT 1',
                       layer.schema_name, layer.table_name) INTO original;
        IF original IS NULL THEN RAISE EXCEPTION 'Empty fixture %.%', layer.schema_name, layer.table_name; END IF;
        factor := 0.1 / greatest(ST_XMax(original) - ST_XMin(original), ST_YMax(original) - ST_YMin(original), 0.1);
        SELECT string_agg(format('%I', column_name), ', ' ORDER BY ordinal_position),
               string_agg(CASE WHEN column_name = 'geom' THEN '$1' ELSE format('%I', column_name) END, ', ' ORDER BY ordinal_position)
          INTO columns_sql, values_sql
          FROM information_schema.columns
          WHERE table_schema = layer.schema_name AND table_name = layer.table_name AND column_name <> 'id';
        FOR target IN SELECT * FROM (VALUES (0.0, 51.5), (-75.0, 0.0), (178.0, 67.5), (-178.0, 67.5), (0.0, 87.5), (0.0, -87.5)) AS positions(lon, lat)
        LOOP
            sample := ST_Scale(original, factor, factor);
            sample := ST_Translate(sample, target.lon - (ST_XMin(sample) + ST_XMax(sample)) / 2,
                                          target.lat - (ST_YMin(sample) + ST_YMax(sample)) / 2);
            IF GeometryType(sample) <> GeometryType(original) OR ST_NDims(sample) <> ST_NDims(original) THEN
                RAISE EXCEPTION 'Fixture geometry type or dimensionality changed for %.%', layer.schema_name, layer.table_name;
            END IF;
            EXECUTE format('INSERT INTO %I.%I (%s) SELECT %s FROM %I.%I ORDER BY id LIMIT 1',
                           layer.schema_name, layer.table_name, columns_sql, values_sql,
                           layer.schema_name, layer.table_name) USING sample;
        END LOOP;
    END LOOP;
    IF layer_count <> 16 THEN RAISE EXCEPTION 'Expected all 16 feature collections, prepared %', layer_count; END IF;
END $$;
