package command

import (
	"time"

	"github.com/kache-store/kache/internal/engine"
	"github.com/kache-store/kache/internal/protocol"
)

// Command represents a client operation that can be executed against the KV Engine.
// Each concrete command is a self-contained object (Command Pattern).
type Command interface {
	Execute(eng engine.Engine) *protocol.Response
}

// Executor routes parsed protocol commands to concrete Command implementations.
type Executor struct {
	engine engine.Engine
}

// NewExecutor creates a new command executor.
func NewExecutor(eng engine.Engine) *Executor {
	return &Executor{engine: eng}
}

// Execute converts a protocol command to a concrete Command and executes it.
func (e *Executor) Execute(cmd *protocol.Command) *protocol.Response {
	switch cmd.Type {
	case protocol.CmdPing:
		return PingCmd{}.Execute(e.engine)
	case protocol.CmdGet:
		return GetCmd{Key: cmd.Key}.Execute(e.engine)
	case protocol.CmdSet:
		return SetCmd{Key: cmd.Key, Value: cmd.Value, TTL: cmd.TTL}.Execute(e.engine)
	case protocol.CmdDel:
		return DelCmd{Key: cmd.Key}.Execute(e.engine)
	case protocol.CmdExpire:
		return ExpireCmd{Key: cmd.Key, TTL: cmd.TTL}.Execute(e.engine)
	case protocol.CmdKeys:
		return KeysCmd{Pattern: cmd.Pattern}.Execute(e.engine)
	case protocol.CmdRange:
		return RangeCmd{Start: cmd.StartKey, End: cmd.EndKey}.Execute(e.engine)
	default:
		return protocol.NewErrorResponse("unknown command")
	}
}

// --- Concrete command implementations ---

// PingCmd responds with PONG.
type PingCmd struct{}

func (c PingCmd) Execute(_ engine.Engine) *protocol.Response {
	return protocol.NewValueResponse([]byte("PONG"))
}

// GetCmd retrieves a value by key.
type GetCmd struct {
	Key string
}

func (c GetCmd) Execute(eng engine.Engine) *protocol.Response {
	val, err := eng.Get(c.Key)
	if err != nil {
		return protocol.NewErrorResponse(err.Error())
	}
	if val == nil {
		return protocol.NewNilResponse()
	}
	return protocol.NewValueResponse(val)
}

// SetCmd stores a key-value pair, optionally with a TTL.
type SetCmd struct {
	Key   string
	Value []byte
	TTL   time.Duration
}

func (c SetCmd) Execute(eng engine.Engine) *protocol.Response {
	var err error
	if c.TTL > 0 {
		err = eng.SetWithTTL(c.Key, c.Value, c.TTL)
	} else {
		err = eng.Set(c.Key, c.Value)
	}
	if err != nil {
		return protocol.NewErrorResponse(err.Error())
	}
	return protocol.NewOKResponse()
}

// DelCmd deletes a key.
type DelCmd struct {
	Key string
}

func (c DelCmd) Execute(eng engine.Engine) *protocol.Response {
	if err := eng.Delete(c.Key); err != nil {
		return protocol.NewErrorResponse(err.Error())
	}
	return protocol.NewOKResponse()
}

// ExpireCmd sets a TTL on an existing key.
type ExpireCmd struct {
	Key string
	TTL time.Duration
}

func (c ExpireCmd) Execute(eng engine.Engine) *protocol.Response {
	if err := eng.Expire(c.Key, c.TTL); err != nil {
		return protocol.NewErrorResponse(err.Error())
	}
	return protocol.NewOKResponse()
}

// KeysCmd lists keys matching a glob pattern.
type KeysCmd struct {
	Pattern string
}

func (c KeysCmd) Execute(eng engine.Engine) *protocol.Response {
	keys := eng.Keys(c.Pattern)
	items := make([][]byte, len(keys))
	for i, k := range keys {
		items[i] = []byte(k)
	}
	return protocol.NewArrayResponse(items)
}

// RangeCmd returns key-value pairs in a lexicographic range.
type RangeCmd struct {
	Start string
	End   string
}

func (c RangeCmd) Execute(eng engine.Engine) *protocol.Response {
	kvs := eng.Range(c.Start, c.End)
	// Encode as array: [key1, val1, key2, val2, ...]
	items := make([][]byte, 0, len(kvs)*2)
	for _, kv := range kvs {
		items = append(items, []byte(kv.Key), kv.Value)
	}
	return protocol.NewArrayResponse(items)
}
