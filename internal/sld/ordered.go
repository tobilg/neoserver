package sld

import (
	"encoding/xml"
	"fmt"
)

// UnmarshalXML preserves cross-type symbolizer ordering, which encoding/xml
// cannot represent using independent typed slices.
func (r *Rule) UnmarshalXML(dec *xml.Decoder, start xml.StartElement) error {
	*r = Rule{}
	for {
		tok, err := dec.Token()
		if err != nil {
			return err
		}
		switch token := tok.(type) {
		case xml.StartElement:
			switch token.Name.Local {
			case "Name":
				if err := dec.DecodeElement(&r.Name, &token); err != nil {
					return err
				}
			case "Title":
				if err := dec.DecodeElement(&r.Title, &token); err != nil {
					return err
				}
			case "Filter":
				var value Filter
				if err := dec.DecodeElement(&value, &token); err != nil {
					return err
				}
				r.Filter = &value
			case "ElseFilter":
				var value struct{}
				if err := dec.DecodeElement(&value, &token); err != nil {
					return err
				}
				r.ElseFilter = &value
			case "MinScaleDenominator":
				if err := dec.DecodeElement(&r.MinScaleDenom, &token); err != nil {
					return err
				}
			case "MaxScaleDenominator":
				if err := dec.DecodeElement(&r.MaxScaleDenom, &token); err != nil {
					return err
				}
			case "PointSymbolizer":
				var value PointSymbolizer
				if err := dec.DecodeElement(&value, &token); err != nil {
					return err
				}
				r.PointSymbolizers = append(r.PointSymbolizers, value)
				r.Symbolizers = append(r.Symbolizers, RawSymbolizer{Point: &r.PointSymbolizers[len(r.PointSymbolizers)-1]})
			case "LineSymbolizer":
				var value LineSymbolizer
				if err := dec.DecodeElement(&value, &token); err != nil {
					return err
				}
				r.LineSymbolizers = append(r.LineSymbolizers, value)
				r.Symbolizers = append(r.Symbolizers, RawSymbolizer{Line: &r.LineSymbolizers[len(r.LineSymbolizers)-1]})
			case "PolygonSymbolizer":
				var value PolygonSymbolizer
				if err := dec.DecodeElement(&value, &token); err != nil {
					return err
				}
				r.PolygonSymbolizer = append(r.PolygonSymbolizer, value)
				r.Symbolizers = append(r.Symbolizers, RawSymbolizer{Polygon: &r.PolygonSymbolizer[len(r.PolygonSymbolizer)-1]})
			case "TextSymbolizer":
				var value TextSymbolizer
				if err := dec.DecodeElement(&value, &token); err != nil {
					return err
				}
				r.TextSymbolizers = append(r.TextSymbolizers, value)
				r.Symbolizers = append(r.Symbolizers, RawSymbolizer{Text: &r.TextSymbolizers[len(r.TextSymbolizers)-1]})
			case "RasterSymbolizer":
				var value RasterSymbolizer
				if err := dec.DecodeElement(&value, &token); err != nil {
					return err
				}
				r.RasterSymbolizers = append(r.RasterSymbolizers, value)
				r.Symbolizers = append(r.Symbolizers, RawSymbolizer{Raster: &r.RasterSymbolizers[len(r.RasterSymbolizers)-1]})
			default:
				return fmt.Errorf("unsupported SLD rule element %q", token.Name.Local)
			}
		case xml.EndElement:
			if token.Name == start.Name {
				return nil
			}
		}
	}
}
