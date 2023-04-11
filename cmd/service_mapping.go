package cmd

// Copyright 2022 Beijing Volcanoengine Technology Ltd.  All Rights Reserved.

var (
	serviceMapping = map[string]string{
		//"rds_mysql_v2": "rds_mysql",
	}

	svcVersionMapping = map[string]map[string]string{
		//"rds_mysql": {
		//	"2022-01-01": "rds_mysql_v2",
		//},
	}
)

func SetServiceMapping(s1, s2 string) {
	serviceMapping[s1] = s2
}

func GetServiceMapping(s string) (string, bool) {
	if v, ok := serviceMapping[s]; ok {
		return v, true
	}
	return s, false
}

func GetSvcVersionMapping(svc, version string) (string, bool) {
	if v, ok := svcVersionMapping[svc]; ok {
		if v1, ok1 := v[version]; ok1 {
			return v1, true
		} else {
			return svc, false
		}
	}
	return svc, false
}
