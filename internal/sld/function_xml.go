package sld

import "encoding/xml"

// UnmarshalXML retains function argument order while preserving the legacy
// typed slices consumed by rendering transformations.
func (f *OGCFunction) UnmarshalXML(decoder *xml.Decoder, start xml.StartElement) error {
	*f = OGCFunction{}
	for _, attribute := range start.Attr {
		if attribute.Name.Local == "name" {
			f.Name = attribute.Value
		}
	}
	for {
		token, err := decoder.Token()
		if err != nil {
			return err
		}
		switch value := token.(type) {
		case xml.StartElement:
			switch value.Name.Local {
			case "Literal":
				var literal OGCLiteral
				if err = decoder.DecodeElement(&literal, &value); err != nil {
					return err
				}
				f.Literals = append(f.Literals, literal)
				f.Arguments = append(f.Arguments, OGCArgument{Literal: literal.Value})
			case "PropertyName":
				var property string
				if err = decoder.DecodeElement(&property, &value); err != nil {
					return err
				}
				f.Properties = append(f.Properties, property)
				f.Arguments = append(f.Arguments, OGCArgument{Property: property})
			case "Function":
				var child OGCFunction
				if err = decoder.DecodeElement(&child, &value); err != nil {
					return err
				}
				f.Functions = append(f.Functions, child)
				childCopy := child
				f.Arguments = append(f.Arguments, OGCArgument{Function: &childCopy})
			}
		case xml.EndElement:
			if value.Name == start.Name {
				return nil
			}
		}
	}
}
