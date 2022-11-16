package cli

// Copyright 2022 Beijing Volcanoengine Technology Ltd.  All Rights Reserved.

type VestackMeta struct {
	ApiInfo  *ApiInfo
	Request  *MetaInfo
	Response *MetaInfo
}

type MetaInfo struct {
	Basic     *[]string
	Structure *map[string]MetaInfo
}

type ApiInfo struct {
	Method      string
	ContentType string
	ServiceName string
	ParamTypes  map[string]string
	// int float64
	// [], {}
}
