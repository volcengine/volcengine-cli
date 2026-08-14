package cmd

// Copyright 2022 Beijing Volcanoengine Technology Ltd.  All Rights Reserved.

import (
	"fmt"
)

type Flag struct {
	Name  string
	value string
	// prefix records how the flag was written on the CLI (-- vs ---) for error messages.
	prefix string
	// multi enables value accumulation across repeated flags (e.g. --header).
	// Single-value flags overwrite on SetValue.
	multi  bool
	values []string
}

// SetValue assigns a value. Multi-value flags append; single-value flags replace.
func (f *Flag) SetValue(value string) {
	f.value = value
	if f.multi {
		f.values = append(f.values, value)
		return
	}
	f.values = []string{value}
}

func (f *Flag) GetValue() string {
	return f.value
}

// GetValues returns all assigned values in order.
func (f *Flag) GetValues() []string {
	if f == nil {
		return nil
	}
	if len(f.values) > 0 {
		out := make([]string, len(f.values))
		copy(out, f.values)
		return out
	}
	if f.value != "" {
		return []string{f.value}
	}
	return nil
}

type FlagSet struct {
	flags []*Flag
	index map[string]*Flag
}

func NewFlagSet() *FlagSet {
	return &FlagSet{
		flags: []*Flag{},
		index: make(map[string]*Flag),
	}
}

func (fs *FlagSet) GetFlags() []*Flag {
	return fs.flags
}

func (fs *FlagSet) AddFlag(f *Flag) {
	if f.Name != "" {
		key := "--" + f.Name
		if _, ok := fs.index[key]; ok {
			panic(fmt.Errorf("Flag is duplicated %s. ", key))
		}
		fs.index[key] = f
		fs.flags = append(fs.flags, f)
	}
}

func (fs *FlagSet) AddByName(name string) (*Flag, error) {
	if isMultiValueDynamicFlag(name) {
		if existing := fs.GetByName(name); existing != nil {
			return existing, nil
		}
	}
	f := &Flag{
		Name:  name,
		multi: isMultiValueDynamicFlag(name),
	}
	if _, ok := fs.index["--"+name]; ok {
		return nil, fmt.Errorf("flag duplicated --%s", name)
	}
	fs.AddFlag(f)
	return f, nil
}

// GetByName returns the flag with the given name, or nil if not found.
func (fs *FlagSet) GetByName(name string) *Flag {
	return fs.index["--"+name]
}
