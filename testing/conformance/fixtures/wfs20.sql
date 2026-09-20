-- Only applied to the disposable WFS ETS database, before discovery.
-- The sampler stops at its first matching XSD type (int, also used by id),
-- so provide an application integer property that is emitted in GML.
ALTER TABLE public.areas ADD COLUMN ets_number integer;
ALTER TABLE public.areas ADD COLUMN ets_time timestamptz;
ALTER TABLE public.areas ADD COLUMN ets_nullable text;
UPDATE public.areas SET ets_number = id * 10,
    ets_time = timestamptz '2000-01-01 00:00:00+00' + id * interval '1 day',
    ets_nullable = CASE WHEN id % 2 = 0 THEN 'present' END;

ALTER TABLE public.places ADD COLUMN ets_number integer;
ALTER TABLE public.places ADD COLUMN ets_time timestamptz;
ALTER TABLE public.places ADD COLUMN ets_nullable text;
UPDATE public.places SET ets_number = id * 10,
    ets_time = timestamptz '2000-01-01 00:00:00+00' + id * interval '1 day',
    ets_nullable = CASE WHEN id % 2 = 0 THEN 'present' END;

ALTER TABLE cite."Ponds" ADD COLUMN ets_nullable text;
UPDATE cite."Ponds" SET ets_nullable = CASE WHEN id % 2 = 0 THEN 'present' END;
