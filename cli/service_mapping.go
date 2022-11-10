package cli

var serviceMapping map[string]string

func InitServiceMapping() {
	serviceMapping = map[string]string{
		"rds_mysql_v2": "rds_mysql",
	}
}

func GetServiceMapping(s string) (string, bool) {
	if v, ok := serviceMapping[s]; ok {
		return v, true
	}
	return s, false
}
