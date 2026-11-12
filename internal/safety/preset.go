package safety

import "sort"

// Official presets.
const (
	// PresetConfirmWrites allows read APIs and requires confirmation for
	// every other operation.
	PresetConfirmWrites = "confirm-writes"
	// PresetConfirmDeletes requires confirmation for destructive or
	// irreversible operations and allows everything else.
	PresetConfirmDeletes = "confirm-deletes"
	// PresetDenyWrites is confirm-writes with every confirm rule turned into
	// deny: read APIs only, and no approval unlocks anything else.
	PresetDenyWrites = "deny-writes"
)

// sensitiveReads are read APIs that issue credentials or tokens, reveal
// secrets or private keys, open console sessions, return console output, or
// hand out download or upload URLs for data. They change no resource but
// grant access beyond reading, so confirm-writes asks before them.
var sensitiveReads = []string{
	"kms:GetSecretValue", "kms:BatchGetSecretValue",
	"metakms:GetSecretValue", "metakms:BatchGetSecretValue",
	"bytehousece20240831:GetUserInitialPassword", "rocketmq:DescribeSecretKey",
	"rdsmysqlv2:DescribeBackupDecryptionKey", "livesaas20230801:GetActivityLoginSecret",
	"ark:GetApiKey", "bio:GetAPIAccessKey",
	"id:GetResourceApiKey", "apig:GetJwtToken",
	"cr:GetAuthorizationToken", "id:GetWorkloadAccessToken",
	"id:GetWorkloadAccessTokenForJWT", "id:GetWorkloadAccessTokenForUserId",
	"id:GetRoleCredentials", "id:GetResourceOauth2Token",
	"id:GetUserCredential", "livesaas:GetTemporaryLoginTokenAPI",
	"livesaas:GetSDKTokenAPI", "livesaas20230801:GetInviterToken",
	"livesaas20230801:GetLoginLivesaasSts", "livesaas20230801:GetVodPlayerToken",
	"ecs:DescribeInstanceECSTerminalUrl", "ecs:DescribeInstanceVncUrl",
	"veenedge:GetVNCUrl", "acep:GetPreSignedEdgeURL",
	"bio:GetImportWorkspacePreSignedURL", "vefaas:GetCodeUploadAddress",
	"vms:GetResourceUploadUrl", "volcsms:GetUploadTosURL",
	"acep:GetEdgeTunnelConnectionInfo", "acep:ListAdbKey",
	"aidap:DescribeAPIKeys", "aidap:DescribeDBAccountConnection",
	"aidap:DescribeDataLakeSecretConnection", "aidap:ListDatabasesDataLakeSecretConnection",
	"aidap:GetSchemaDiffResultDownloadLink", "aiotvideo:GetLocalDownload",
	"apig:ListConsumerCredentials", "ark:GetEndpointCertificate",
	"arkclaw:GetUserPoolClient", "bio:GetExportWorkspacePreSignedURL",
	"bmq20240901:DescribeDeviceCredential", "certificateservice:CertificateGetInstance",
	"certificateservice:CodeSignCertificateGetInstance", "cloudidentity:ListSCIMServerCredentials",
	"cp:GetServiceConnection", "cp:ListServiceConnections",
	"cp:GetTaskRunLogDownloadURI", "ecs:GetConsoleOutput",
	"ecs:GetConsoleScreenshot", "iam:ListApiKeys",
	"id:GetApiKeyCredentialProvider", "id:BatchGetApiKeyCredentialProviders",
	"id:GetOauth2CredentialProvider", "id:BatchGetOauth2CredentialProviders",
	"id:GetRoleCredentialProvider", "id:GetSharedOauth2Credential",
	"id:ListSharedOauth2Credentials", "id:ListSCIMTokens",
	"id:GetInboundAuthConfig", "id:BatchGetInboundAuthConfig",
	"id:GetIdentityProviderOAuth", "id:GetUserPoolIamCredentialsServiceConfig",
	"id:GetUserPoolClient", "id:ListUserPoolClients",
	"livesaas20230801:GetActivityExportFile", "mem0:DescribeAPIKeyDetail",
	"mongodb:DescribeDBInstanceBackupURL", "rdsmysqlv2:GetBackupDownloadLink",
	"rdspostgresql:GetBackupDownloadLink", "redis:DescribeBackupPointDownloadUrls",
	"pca:DownloadLeafInstance", "rocketmq:DescribeAccessKeyDetail",
	"rocketmq:DescribeAccessKeys", "secagent:GetResourceAuthConfig",
	"seccenter20240508:GetVarmorAuthInfo", "seccenter20240508:GetRepoRegistryVpcAuthCreateInfo",
	"speechsaasprod:ListAPIKeys", "vefaas:ListE2BAPIKeys",
	"vefaas:GetSandboxImagePrecacheTicket", "vke:ListKubeconfigs",
	"vmp:GetWorkspaceAuthInfo", "vms:QueryAudioRecordFileUrl",
	"vms:QueryAudioRecordToTextFileUrlV2", "vpn:DescribeSslVpnClientCertAttributes",
	"vpn:DescribeVpnConnectionAttributes", "vpn:DescribeVpnConnections",
}

// writeLikeReads are APIs named like reads that generate content,
// parameters or tasks.
var writeLikeReads = []string{
	"waf:QueryLLMGenerate", "livesaas:QueryUploadMediaByURL", "kms:GetParametersForImport",
}

// readPrefixes are action verbs that only read. Matching is
// case-insensitive, so lowerCamel actions such as spark:listApplication match.
var readPrefixes = []string{
	"Describe", "Desc", "List", "Get", "Query", "Search", "Show", "Detail",
	"Lookup", "Check", "Calc", "Stat", "BatchGet", "BatchList", "Analysis",
}

// readActions are read APIs whose names do not start with a read verb.
var readActions = []string{
	"bmq:GroupExist", "bmq:TopicExist",
	"spark:existResourcePool", "cloudtrail:LookupEvents",
	"certificateservice:CertificateGetDcvParam", "certificateservice:CertificateGetOrganization",
	"certificateservice:CertificateGetOrganizationList", "certificateservice:CertificateGetInstanceList",
	"certificateservice:CodeSignCertificateListInstances", "cv20240606:FaceFusionMovieGetResult",
	"cv20240606:LensVidaVideoGetResult", "cv20240606:LensVideoNnsrGetResult",
	"cv20240606:AIGCStylizeImageUsage", "dataleap:DTSOpenDescribeResourceGroups",
	"dataleap:DTSOpenListTagResourceGroups", "dbw:SlowQueryAdviceTaskHistoryApi",
	"fwcenter:AssetList", "graph:DecodeRawKey",
	"i18nopenapi:Projects", "i18nopenapi:Languages",
	"i18nopenapi:MtSupportLang", "i18nopenapi:TermBaseTermGroups",
	"i18nopenapi:ProjectDetail", "i18nopenapi:ProjectNamespaceDetail",
	"i18nopenapi:ProjectNamespaceSourceDetail", "i18nopenapi:ProjectTaskDetail",
	"i18nopenapi:ProjectTaskSourceDetail", "i18nopenapi:ProjectTermDetail",
	"i18nopenapi:ProjectTaskTextCount", "i18nopenapi:ProjectOperableList",
	"i18nopenapi:ProjectTextImportStatusQuery", "i18nopenapi:ProjectDownloadProgressQuery",
	"i18nopenapi:DocumentTaskDetail", "i18nopenapi:TextGetTextListWithKeyOrSource",
	"i18nopenapi:VideoProjectList", "i18nopenapi:VideoProjectTaskList",
	"i18nopenapi:VideoProjectTaskDetail", "i18nopenapi:VideoProjectGetToolTask",
	"i18nopenapi:VideoProjectGetVideoUploadStatus", "i18nopenapi:VideoProjectGetUploadSubtitleStatus",
	"i18nopenapi:VideoProjectGetTaskProduct", "i18nopenapi:VideoProjectListToolProjectResources",
	"i18nopenapi:VideoProjectListToolTaskByResourceIDs", "i18nopenapi:VideoProjectListVideoDrama",
	"i18nopenapi:VideoEditorListSubtitles", "i18nopenapi:VideoEditorGetEmotionTags",
	"i18nopenapi:VideoEditorGetSpeakers", "i18nopenapi:VideoEditorQueryAIMTBySegmentResult",
	"i18nopenapi:VideoEditorQueryAsyncGenDubbingResult", "mcs:PostApiV1AssetDescribeDetail",
	"mcs:PostApiV1OverviewDescribeAssetInfo", "mcs:PostApiV1AlarmDescribeOverview",
	"milvus:MSDescribeInstances", "milvus:MSDescribeInstance",
	"milvus:DMDescribeWorkflows", "milvus:DMDescribeWorkflow",
	"seccenter20240508:MultiCloudAccessSyncStatus", "seccenter20240508:MultiCloudAccessStatistics",
	"seccenter20240508:PrecisionStatistics", "speechsaasprod:ResourcePacksStatus",
	"speechsaasprod:ServiceStatus", "speechsaasprod20250521:QuotaMonitoring",
	"speechsaasprod20250521:UsageMonitoring", "vikingdb:MemoryCollectionInfo",
	"vikingdb:MemoryCollectionList", "vms:NumberList",
	"vms:NumberPoolList", "volccontentplatform:TagListArticleTag",
	"volccontentplatform:TagListArticleTagByOpenId", "volccontentplatform:TagListTagTree",
	"vefaasdev:ReadFiles", "bmq:PreviewTopicData",
	"rdsmssql:DownloadSSLCertificate", "rdsmysqlv2:DownloadSSLCertificate",
	"rdspostgresql:DownloadSSLCertificate", "dts:DownloadValidationTaskResult",
}

// destructivePatterns match destructive or irreversible operations. They err
// on matching too much: confirm-deletes allows reads before them.
var destructivePatterns = []string{
	"*:*Delet*", "*:Del*", "*:BatchDel*", "*:*Remove*", "*:*Terminate*", "*:*Destroy*", "*:*Uninstall*",
	"*:Release*", "*:MSRelease*", "*:Drop*", "*:Truncate*", "*:Flush*", "*:*Purge*",
	"*:Clean*", "*:Clear*", "*:Kill*", "*:Revoke*", "*:*Rollback*", "*:*Restore*",
	"*:Reset*", "*:BatchReset*", "*:Unsubscribe*", "*:Deregister*", "*:Quit*", "*:ReplaceSystemVolume",
	"*:ScaleIn*", "*:ScaleDown*", "*:Decrease*", "*:*Command*", "*:Execute*", "*:ForwardKubernetesApi",
}

// presets maps a preset name to its rules.
var presets = map[string]func() []Rule{
	PresetConfirmWrites:  confirmWritesRules,
	PresetConfirmDeletes: confirmDeletesRules,
	PresetDenyWrites:     func() []Rule { return DenyConfirmRules(confirmWritesRules()) },
}

// PresetNames returns the names of the official presets.
func PresetNames() []string {
	names := make([]string, 0, len(presets))
	for name := range presets {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// PresetRules returns the rules of an official preset in match order, and
// false for an unknown name.
func PresetRules(name string) ([]Rule, bool) {
	build, ok := presets[name]
	if !ok {
		return nil, false
	}
	return build(), true
}

func readAllowRules() []Rule {
	rules := make([]Rule, 0, len(readPrefixes))
	for _, prefix := range readPrefixes {
		rules = append(rules, Rule{Pattern: "*:" + prefix + "*", Action: ActionAllow})
	}
	return rules
}

// confirmWritesRules lists, in match order, the sensitive and write-like
// reads to confirm, the read verbs and actions to allow, then confirmation
// for everything else, including services added later.
func confirmWritesRules() []Rule {
	var rules []Rule
	for _, pattern := range append(append([]string{}, sensitiveReads...), writeLikeReads...) {
		rules = append(rules, Rule{Pattern: pattern, Action: ActionConfirm})
	}
	rules = append(rules, readAllowRules()...)
	for _, pattern := range readActions {
		rules = append(rules, Rule{Pattern: pattern, Action: ActionAllow})
	}
	return append(rules, Rule{Pattern: "*:*", Action: ActionConfirm})
}

// confirmDeletesRules allows the read verbs, so reads never ask, then
// requires confirmation for destructive operations; anything else matches no
// rule and is allowed.
func confirmDeletesRules() []Rule {
	rules := readAllowRules()
	for _, pattern := range destructivePatterns {
		rules = append(rules, Rule{Pattern: pattern, Action: ActionConfirm})
	}
	return rules
}

// DenyConfirmRules returns a copy of rules with every confirm rule turned into
// deny, keeping the match order. It derives deny-writes from confirm-writes,
// and a read-only policy from any confirm policy.
func DenyConfirmRules(rules []Rule) []Rule {
	denied := make([]Rule, 0, len(rules))
	for _, rule := range rules {
		if rule.Action == ActionConfirm {
			rule.Action = ActionDeny
		}
		denied = append(denied, rule)
	}
	return denied
}
