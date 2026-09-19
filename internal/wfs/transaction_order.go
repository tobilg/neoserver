package wfs

import (
	"encoding/xml"
	"fmt"
	"io"
)

type transactionOperation struct {
	kind  string
	index int
}

// UnmarshalXML retains action order. The typed slices remain available to
// programmatic callers, but decoded requests execute only this ordered index.
func (tx *WFSTransaction) UnmarshalXML(d *xml.Decoder, start xml.StartElement) error {
	if start.Name.Local != "Transaction" || (start.Name.Space != "" && start.Name.Space != NSWfs) {
		return fmt.Errorf("expected a WFS Transaction element")
	}
	*tx = WFSTransaction{XMLName: start.Name, operations: []transactionOperation{}}
	for _, a := range start.Attr {
		switch a.Name.Local {
		case "version":
			tx.Version = a.Value
		case "service":
			tx.Service = a.Value
		case "handle":
			tx.Handle = a.Value
		case "lockId":
			tx.LockId = a.Value
		case "releaseAction":
			tx.ReleaseAction = a.Value
		case "srsName":
			tx.SrsName = a.Value
		}
	}
	for {
		token, err := d.Token()
		if err != nil {
			if err == io.EOF {
				return io.ErrUnexpectedEOF
			}
			return err
		}
		switch t := token.(type) {
		case xml.StartElement:
			if t.Name.Space != "" && t.Name.Space != NSWfs {
				return fmt.Errorf("invalid transaction action namespace %q", t.Name.Space)
			}
			op := transactionOperation{kind: t.Name.Local}
			switch t.Name.Local {
			case "Insert":
				var v WFSInsert
				if err := d.DecodeElement(&v, &t); err != nil {
					return err
				}
				op.index = len(tx.Inserts)
				tx.Inserts = append(tx.Inserts, v)
			case "Update":
				var v WFSUpdate
				if err := d.DecodeElement(&v, &t); err != nil {
					return err
				}
				op.index = len(tx.Updates)
				tx.Updates = append(tx.Updates, v)
			case "Delete":
				var v WFSDelete
				if err := d.DecodeElement(&v, &t); err != nil {
					return err
				}
				op.index = len(tx.Deletes)
				tx.Deletes = append(tx.Deletes, v)
			case "Replace":
				var v WFSReplace
				if err := d.DecodeElement(&v, &t); err != nil {
					return err
				}
				op.index = len(tx.Replaces)
				tx.Replaces = append(tx.Replaces, v)
			case "Native":
				var v WFSNativeElement
				if err := d.DecodeElement(&v, &t); err != nil {
					return err
				}
				if !v.SafeToIgnore {
					return fmt.Errorf("unsupported Native action")
				}
				continue
			default:
				return fmt.Errorf("unsupported transaction action %q", t.Name.Local)
			}
			tx.operations = append(tx.operations, op)
		case xml.EndElement:
			if t.Name == start.Name {
				return nil
			}
		}
	}
}

func (tx *WFSTransaction) orderedOperations() []transactionOperation {
	if tx.operations != nil {
		return tx.operations
	}
	// Struct literals have no XML document order; preserve their historical
	// construction order. All HTTP requests use the decoder above.
	var ops []transactionOperation
	for _, group := range []struct {
		name string
		size int
	}{{"Insert", len(tx.Inserts)}, {"Update", len(tx.Updates)}, {"Delete", len(tx.Deletes)}, {"Replace", len(tx.Replaces)}} {
		for i := 0; i < group.size; i++ {
			ops = append(ops, transactionOperation{group.name, i})
		}
	}
	return ops
}
