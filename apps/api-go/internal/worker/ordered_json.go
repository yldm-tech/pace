package worker

import (
	"bytes"
	"encoding/json"
)

// field is one key and its value on the way into an object whose order matters.
type field struct {
	Name  string
	Value any
}

// orderedJSON writes an object with its keys in the order they were given.
//
// A Go map has no order and marshalling one sorts the keys, which would change the bytes a webhook receiver sees against what the Python side produced. These objects are a serializer's output and their order is part of what was sent, so they are written by hand.
func orderedJSON(fields []field) (json.RawMessage, error) {
	var buffer bytes.Buffer
	buffer.WriteByte('{')
	for index, entry := range fields {
		if index > 0 {
			buffer.WriteString(", ")
		}
		name, err := json.Marshal(entry.Name)
		if err != nil {
			return nil, err
		}
		buffer.Write(name)
		buffer.WriteString(": ")
		if raw, ok := entry.Value.(json.RawMessage); ok {
			if len(raw) == 0 {
				buffer.WriteString("null")
			} else {
				buffer.Write(raw)
			}
			continue
		}
		value, err := json.Marshal(entry.Value)
		if err != nil {
			return nil, err
		}
		buffer.Write(value)
	}
	buffer.WriteByte('}')
	return buffer.Bytes(), nil
}

// orderedList writes a list of objects that were each written in order.
func orderedList(items []json.RawMessage) json.RawMessage {
	var buffer bytes.Buffer
	buffer.WriteByte('[')
	for index, item := range items {
		if index > 0 {
			buffer.WriteString(", ")
		}
		buffer.Write(item)
	}
	buffer.WriteByte(']')
	return buffer.Bytes()
}
