package cmd

// Copyright 2022 Beijing Volcanoengine Technology Ltd.  All Rights Reserved.

import (
	"fmt"
)

type Flag struct {
	Name  string
	value string
}

func (f *Flag) SetValue(value string) {
	f.value = value
}

func (f *Flag) GetValue() string {
	return f.value
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
	f := &Flag{
		Name: name,
	}
	if _, ok := fs.index["--"+name]; ok {
		return nil, fmt.Errorf("flag duplicated --%s", name)
	}
	fs.AddFlag(f)
	return f, nil
}
