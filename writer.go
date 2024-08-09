package wire

import (
	"context"
	"errors"

	"github.com/jeroenrinzema/psql-wire/pkg/buffer"
	"github.com/jeroenrinzema/psql-wire/pkg/types"
)

// DataWriter represents a writer interface for writing columns and data rows
// using the Postgres wire to the connected client.
type DataWriter interface {
	// Row writes a single data row containing the values inside the given slice to
	// the underlaying Postgres client. The column headers have to be written before
	// sending rows. Each item inside the slice represents a single column value.
	// The slice length needs to be the same length as the defined columns. Nil
	// values are encoded as NULL values.
	Row([]any) error

	// Written returns the number of rows written to the client.
	Written() uint64

	// Empty announces to the client a empty response and that no data rows should
	// be expected.
	Empty() error

	// Complete announces to the client that the command has been completed and
	// no further data should be expected.
	Complete(description string) error

	// CopyIn is incomplete
	CopyIn() error
}

// ErrDataWritten is thrown when an empty result is attempted to be send to the
// client while data has already been written.
var ErrDataWritten = errors.New("data has already been written")

// ErrClosedWriter is thrown when the data writer has been closed
var ErrClosedWriter = errors.New("closed writer")

// NewDataWriter constructs a new data writer using the given context and
// buffer. The returned writer should be handled with caution as it is not safe
// for concurrent use. Concurrent access to the same data without proper
// synchronization can result in unexpected behavior and data corruption.
func NewDataWriter(ctx context.Context, columns Columns, formats []FormatCode, writer *buffer.Writer) DataWriter {
	return &dataWriter{
		ctx:     ctx,
		columns: columns,
		formats: formats,
		client:  writer,
	}
}

// dataWriter is a implementation of the DataWriter interface.
type dataWriter struct {
	ctx     context.Context
	columns Columns
	formats []FormatCode
	client  *buffer.Writer
	closed  bool
	written uint64
}

func (writer *dataWriter) Define(columns Columns) error {
	if writer.closed {
		return ErrClosedWriter
	}

	writer.columns = columns
	return writer.columns.Define(writer.ctx, writer.client, writer.formats)
}

func (writer *dataWriter) Row(values []any) error {
	if writer.closed {
		return ErrClosedWriter
	}

	writer.written++

	return writer.columns.Write(writer.ctx, writer.formats, writer.client, values)
}

func (writer *dataWriter) CopyIn() error {
	if writer.closed {
		return ErrClosedWriter
	}
	// if writer.reader == nil {
	// 	return errors.New("reader is nil; use PortalCacheCopy to execute CopyIn")
	// }
	writer.client.Start(types.ServerCopyInResponse)
	writer.client.AddByte(0)
	const n = 3
	writer.client.AddInt16(n)
	for i := 0; i < n; i++ {
		writer.client.AddInt16(0)
	}
	if err := writer.client.End(); err != nil {
		return err
	}

	return nil
}

func (writer *dataWriter) Empty() error {
	if writer.closed {
		return ErrClosedWriter
	}

	if writer.written != 0 {
		return ErrDataWritten
	}

	defer writer.close()
	return nil
}

func (writer *dataWriter) Written() uint64 {
	return writer.written
}

func (writer *dataWriter) Complete(description string) error {
	if writer.closed {
		return ErrClosedWriter
	}

	if writer.written == 0 && writer.columns != nil {
		err := writer.Empty()
		if err != nil {
			return err
		}
	}

	defer writer.close()
	return commandComplete(writer.client, description)
}

func (writer *dataWriter) close() {
	writer.closed = true
}

// commandComplete announces that the requested command has successfully been executed.
// The given description is written back to the client and could be used to send
// additional meta data to the user.
func commandComplete(writer *buffer.Writer, description string) error {
	writer.Start(types.ServerCommandComplete)
	writer.AddString(description)
	writer.AddNullTerminate()
	return writer.End()
}
