package safety

import "sort"

// PresetReadOnly is the official read-only preset: read APIs are allowed and
// every other operation needs confirmation.
const PresetReadOnly = "readonly"

// readOnlySensitive are read APIs that issue credentials or tokens, reveal
// secrets, open console sessions or hand out upload URLs. They change no
// resource but grant access beyond reading, so the read-only preset asks for
// confirmation before them.
var readOnlySensitive = []string{
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
	"aiotvideo20231001:DownloadLocalMedia",
}

// readOnlyPrefixes are action verbs that only read. Matching is
// case-insensitive, so lowerCamel actions such as spark:listApplication match.
var readOnlyPrefixes = []string{
	"Describe", "Desc", "List", "Get", "Query", "Search",
	"Show", "Detail", "Lookup", "Check", "Preview", "Precheck",
	"Calc", "Stat", "BatchGet", "BatchList", "Download", "Analysis",
}

// readOnlyActions are read APIs whose names do not start with a read verb.
var readOnlyActions = []string{
	"bmq:GroupExist", "bmq:TopicExist",
	"spark:existResourcePool", "cloudtrail:LookupEvents",
	"certificateservice:CertificateGetDcvParam", "certificateservice:CertificateGetOrganization",
	"certificateservice:CertificateGetOrganizationList", "certificateservice:CertificateGetInstance",
	"certificateservice:CertificateGetInstanceList", "certificateservice:CodeSignCertificateGetInstance",
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
	"livesaas20230801:ExportActivityData", "veenedge:ExportSecurityGroups",
	"veenedge:ExportEIPInstances", "seccenter20240508:Export*",
	"vefaasdev:ReadFiles",
}

// presets maps a preset name to its rules.
var presets = map[string]func() []Rule{
	PresetReadOnly: readOnlyPresetRules,
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

// readOnlyPresetRules lists, in match order, the sensitive reads to
// confirm, the read-only verbs and actions to allow, then confirmation for
// everything else.
func readOnlyPresetRules() []Rule {
	rules := make([]Rule, 0, len(readOnlySensitive)+len(readOnlyPrefixes)+len(readOnlyActions)+1)
	for _, pattern := range readOnlySensitive {
		rules = append(rules, Rule{Pattern: pattern, Action: ActionConfirm})
	}
	for _, prefix := range readOnlyPrefixes {
		rules = append(rules, Rule{Pattern: "*:" + prefix + "*", Action: ActionAllow})
	}
	for _, pattern := range readOnlyActions {
		rules = append(rules, Rule{Pattern: pattern, Action: ActionAllow})
	}
	return append(rules, Rule{Pattern: "*:*", Action: ActionConfirm})
}
