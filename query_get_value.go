package goriak

import (
	"encoding/json"
	"errors"
	riak "github.com/basho/riak-go-client"
)

type commandGet struct {
	*Command

	// Riak builder type for SetValue
	// Other commands populate riakComand directly
	// SetJSON and SetRaw will populate these values instead
	builder *riak.FetchValueCommandBuilder

	key         string
	output      interface{}
	outputBytes *[]byte

	isRawOutput bool

	// VClock is used in conflict resolution
	// http://docs.basho.com/riak/kv/2.1.4/developing/usage/conflict-resolution/
	vclock               []byte
	conflictResolverFunc func([]ConflictObject) ResolvedConflict
}

func (c *commandGet) ConflictResolver(fn func([]ConflictObject) ResolvedConflict) *commandGet {
	c.conflictResolverFunc = fn
	return c
}

func (c *commandGet) fetchValueWithResolver(session *Session, values []*riak.Object) ([]byte, []byte, error) {

	// Conflict resolution necessary
	if len(values) > 1 {

		// No explicit resolver func
		if c.conflictResolverFunc == nil {

			// Use conflict resolver func from interface
			if resolver, ok := c.output.(ConflictResolver); ok {
				c.conflictResolverFunc = resolver.ConflictResolver
			} else {
				return []byte{}, []byte{}, errors.New("goriak: Had conflict, but no conflict resolver")
			}
		}

		objs := make([]ConflictObject, len(values))

		for i, v := range values {
			objs[i] = ConflictObject{
				Value:        v.Value,
				LastModified: v.LastModified,
				VClock:       v.VClock,
			}
		}

		useObj := c.conflictResolverFunc(objs)

		if len(useObj.VClock) == 0 {
			return []byte{}, []byte{}, errors.New("goriak: Invalid value from conflict resolver")
		}

		// Save resolution
		Bucket(c.bucket, c.bucketType).
			SetRaw(useObj.Value).
			Key(c.key).
			VClock(useObj.VClock).
			Run(session)

		return useObj.Value, useObj.VClock, nil
	}

	return values[0].Value, values[0].VClock, nil
}

func (c *commandGet) resultFetchValueCommandJSON(session *Session, cmd *riak.FetchValueCommand) (*Result, error) {
	if !cmd.Success() {
		return nil, errors.New("Not successful")
	}

	if cmd.Response.IsNotFound {
		return &Result{
			NotFound: true,
		}, errors.New("Not found")
	}

	value, vclock, err := c.fetchValueWithResolver(session, cmd.Response.Values)

	if err != nil {
		return nil, err
	}

	err = json.Unmarshal(value, c.output)

	if err != nil {
		return nil, err
	}

	return &Result{
		Key:    c.key,
		VClock: vclock,
	}, nil
}

func (c *commandGet) resultFetchValueCommandRaw(session *Session, cmd *riak.FetchValueCommand) (*Result, error) {
	if !cmd.Success() {
		return nil, errors.New("Not successful")
	}

	if cmd.Response.IsNotFound {
		return &Result{
			NotFound: true,
		}, errors.New("Not found")
	}

	value, vclock, err := c.fetchValueWithResolver(session, cmd.Response.Values)

	if err != nil {
		return nil, err
	}

	*c.outputBytes = value

	return &Result{
		Key:    c.key,
		VClock: vclock,
	}, nil
}

func (c *commandGet) Run(session *Session) (*Result, error) {
	cmd, err := c.builder.Build()
	if err != nil {
		return nil, err
	}

	err = session.riak.Execute(cmd)
	if err != nil {
		return nil, err
	}

	fetchCmd := cmd.(*riak.FetchValueCommand)

	if !fetchCmd.Success() {
		return nil, errors.New("Not successful")
	}

	if fetchCmd.Response.IsNotFound {
		return &Result{NotFound: true}, errors.New("Not found")
	}

	value, vclock, err := c.fetchValueWithResolver(session, fetchCmd.Response.Values)
	if err != nil {
		return nil, err
	}

	if c.isRawOutput {
		*c.outputBytes = value
	} else {
		err = json.Unmarshal(value, c.output)
		if err != nil {
			return nil, err
		}
	}

	return &Result{
		Key:    c.key,
		VClock: vclock,
	}, nil
}
