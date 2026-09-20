-- WMTS-only dimension values; the base WMS fixture stays unchanged.
ALTER TABLE cite."Autos" ADD COLUMN ets_elevation double precision DEFAULT 100;
UPDATE cite."Autos" SET ets_elevation = CASE WHEN id % 2 = 0 THEN 200 ELSE 100 END;
