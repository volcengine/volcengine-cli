package cmd

// Copyright 2022 Beijing Volcanoengine Technology Ltd.  All Rights Reserved.

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"sync"

	"github.com/volcengine/volcengine-cli/util"
)

var configFileMu sync.Mutex

// ErrConcurrentConfigModification is returned when this process and another
// process changed the same logical config value from a shared baseline.
var ErrConcurrentConfigModification = errors.New("concurrent modification")

var configFileDirFunc = util.GetConfigFileDir

// 定义模式枚举常量
const (
	ModeSSO          = "sso"
	ModeConsoleLogin = "console-login"
	ModeAK           = "ak"
	ModeRamRoleArn   = "ramrolearn"
	ModeOIDC         = "oidc"
	ModeEcsRole      = "ecsrole"
	ConfigFile       = "config.json"
)

type Configure struct {
	Current     string                 `json:"current"`
	Profiles    map[string]*Profile    `json:"profiles"`
	EnableColor bool                   `json:"enableColor"`
	SsoSession  map[string]*SsoSession `json:"sso-session"`
}

// configTransaction keeps persistence-only state out of the exported
// Configure data type, preserving its source-compatible four-field layout.
// baseline is the immutable snapshot observed before config is mutated;
// baselinePath prevents a transaction loaded from one config directory from
// being compared with a different config file.
type configTransaction struct {
	config       *Configure
	baseline     *Configure
	baselinePath string
}

// prepareConfigForMutation normalizes cfg and ensures it has an immutable
// baseline before a long-lived runtime path mutates ctx.config directly.
func prepareConfigForMutation(cfg *Configure) (*configTransaction, error) {
	if cfg == nil {
		return nil, errors.New("config is nil")
	}
	normalizeConfig(cfg)
	return ensureConfigTransaction(cfg)
}

type Profile struct {
	Name             string `json:"name"`
	Mode             string `json:"mode"`
	AccessKey        string `json:"access-key"`
	SecretKey        string `json:"secret-key"`
	Region           string `json:"region"`
	Endpoint         string `json:"endpoint"`
	EndpointResolver string `json:"endpoint-resolver,omitempty"`
	HTTPProxy        string `json:"http-proxy,omitempty"`
	HTTPSProxy       string `json:"https-proxy,omitempty"`
	UseDualStack     *bool  `json:"use-dual-stack,omitempty"`
	SessionToken     string `json:"session-token"`
	DisableSSL       *bool  `json:"disable-ssl"`
	SsoSessionName   string `json:"sso-session-name"`
	AccountId        string `json:"account-id"`
	RoleName         string `json:"role-name"`
	StsExpiration    int64  `json:"sts-expiration"`
	OidcTokenFile    string `json:"oidc-token-file,omitempty"`
	RoleTrn          string `json:"role-trn,omitempty"`
	LoginSession     string `json:"login-session,omitempty"`
}

type SsoSession struct {
	Name               string   `json:"name"`
	StartURL           string   `json:"start-url"`
	Region             string   `json:"region"`
	RegistrationScopes []string `json:"registration-scopes,omitempty"`
}

// LoadConfig from CONFIG_FILE_DIR(default ~/.volcengine)
func LoadConfig() *Configure {
	cfg, _ := loadConfigForRuntime(false)
	return cfg
}

// loadConfigTransaction loads configuration together with the immutable
// baseline used by internal CLI mutation paths. LoadConfig intentionally
// exposes only the data object to preserve its historical public API.
func loadConfigTransaction() *configTransaction {
	cfg, configFilePath := loadConfigForRuntime(true)
	if cfg == nil {
		return nil
	}
	return newConfigTransaction(cfg, cfg, configFilePath)
}

// loadConfigForRuntime preserves LoadConfig's historical raw decoded value
// while allowing internal mutation paths to request initialized maps.
func loadConfigForRuntime(normalize bool) (*Configure, string) {
	configFileMu.Lock()
	defer configFileMu.Unlock()

	configFileDir, err := configFileDirFunc()
	if err != nil {
		return nil, ""
	}

	if err := os.MkdirAll(configFileDir, 0700); err != nil {
		return nil, ""
	}
	_ = os.Chmod(configFileDir, 0700)

	configFilePath := filepath.Join(configFileDir, ConfigFile)
	file, err := os.OpenFile(configFilePath, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		fmt.Println(err)
		return nil, ""
	}
	defer file.Close()
	_ = file.Chmod(0600)

	fileContent, err := ioutil.ReadAll(file)
	if err != nil {
		return nil, ""
	}

	cfg := &Configure{}
	err = json.Unmarshal(fileContent, cfg)
	if err != nil {
		return nil, ""
	}
	if normalize {
		normalizeConfig(cfg)
	}
	return cfg, configFilePath
}

// readConfigFile loads config.json without creating or rewriting it.
// Missing file returns (nil, nil). Existing invalid JSON returns an error so
// callers cannot overwrite a corrupt-but-nonempty file with an empty config.
func readConfigFile() (*Configure, error) {
	configFileDir, err := configFileDirFunc()
	if err != nil {
		return nil, err
	}
	return readConfigFileAtPath(filepath.Join(configFileDir, ConfigFile))
}

func readConfigFileAtPath(configFilePath string) (*Configure, error) {
	fileContent, err := ioutil.ReadFile(configFilePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	if len(bytes.TrimSpace(fileContent)) == 0 {
		return &Configure{
			Profiles:   make(map[string]*Profile),
			SsoSession: make(map[string]*SsoSession),
		}, nil
	}
	cfg := &Configure{}
	if err := json.Unmarshal(fileContent, cfg); err != nil {
		return nil, fmt.Errorf("parse %s: %w", configFilePath, err)
	}
	normalizeConfig(cfg)
	return cfg, nil
}

func configForWrite() (*configTransaction, error) {
	if cfg := runtimeConfig(); cfg != nil {
		normalizeConfig(cfg)
		tx, err := ensureConfigTransaction(cfg)
		if err != nil {
			return nil, err
		}
		setRuntimeConfigTransaction(tx)
		return tx, nil
	}

	configFileDir, err := configFileDirFunc()
	if err != nil {
		return nil, err
	}
	configFilePath := filepath.Join(configFileDir, ConfigFile)

	configFileMu.Lock()
	defer configFileMu.Unlock()
	loaded, err := readConfigFileAtPath(configFilePath)
	if err != nil {
		return nil, err
	}
	if loaded == nil {
		loaded = newEmptyConfig()
	}
	normalizeConfig(loaded)
	tx := newConfigTransaction(loaded, loaded, configFilePath)
	setRuntimeConfigTransaction(tx)
	return tx, nil
}

// ensureConfigTransaction captures disk state before callers mutate a runtime
// config that was constructed programmatically rather than by the internal
// transaction loader.
func ensureConfigTransaction(cfg *Configure) (*configTransaction, error) {
	configFileDir, err := configFileDirFunc()
	if err != nil {
		return nil, err
	}
	configFilePath := filepath.Join(configFileDir, ConfigFile)

	configFileMu.Lock()
	defer configFileMu.Unlock()
	if tx := runtimeConfigTransaction(); tx != nil && tx.config == cfg &&
		sameConfigPath(tx.baselinePath, configFilePath) {
		return tx, nil
	}
	loaded, err := readConfigFileAtPath(configFilePath)
	if err != nil {
		return nil, err
	}
	if loaded == nil {
		loaded = newEmptyConfig()
	}
	tx := newConfigTransaction(cfg, loaded, configFilePath)
	return tx, nil
}

// runtimeConfig returns the in-memory config used by the current CLI process.
// Prefer this over reloading from disk so command handlers operate on a single
// config object during one invocation.
func runtimeConfig() *Configure {
	if ctx != nil && ctx.config != nil {
		return ctx.config
	}
	return config
}

func runtimeConfigTransaction() *configTransaction {
	if ctx == nil || ctx.configTransaction == nil {
		return nil
	}
	if ctx.configTransaction.config != ctx.config {
		return nil
	}
	return ctx.configTransaction
}

// setRuntimeConfig keeps the global config references in sync after updates.
func setRuntimeConfig(cfg *Configure) {
	if ctx != nil {
		if ctx.configTransaction == nil || ctx.configTransaction.config != cfg {
			ctx.configTransaction = nil
		}
		ctx.config = cfg
	}
	config = cfg
}

func setRuntimeConfigTransaction(tx *configTransaction) {
	if tx == nil {
		setRuntimeConfig(nil)
		return
	}
	if ctx != nil {
		ctx.config = tx.config
		ctx.configTransaction = tx
	}
	config = tx.config
}

// WriteConfigToFile stores config using the historical whole-config overwrite
// semantics. It intentionally does not require a baseline: this exported API
// is also used by callers that construct Configure values directly. CLI
// mutation paths use writeConfigTransaction instead.
func WriteConfigToFile(config *Configure) error {
	return writeConfigToFile(config, replaceFile)
}

// writeConfigToFile keeps the replacement operation injectable without
// changing process-global state. Production always passes replaceFile; tests
// can exercise a failure after the temporary file has been fully written.
func writeConfigToFile(config *Configure, replacer func(src, dst string) error) error {
	configFileMu.Lock()
	defer configFileMu.Unlock()
	return writeConfigWithLock(&configTransaction{config: config}, replacer, false)
}

// writeConfigTransaction persists a baseline-bearing CLI mutation with a
// lock-protected three-way merge. On success, and on a PartialCommitError, it
// refreshes config with the committed merged value and advances the transaction
// baseline.
func writeConfigTransaction(tx *configTransaction) error {
	return writeConfigTransactionWithReplacer(tx, replaceFile)
}

// writeConfigTransactionWithReplacer keeps replacement injectable for tests.
func writeConfigTransactionWithReplacer(tx *configTransaction, replacer func(src, dst string) error) error {
	if tx == nil || tx.config == nil {
		return errors.New("config is nil")
	}
	// baseline is the immutable pre-mutation state captured before mutation. A
	// hard, uncommitted failure must not leave the process using changes that
	// never reached disk.
	before := normalizedConfigCopy(tx.baseline)
	configFileMu.Lock()
	defer configFileMu.Unlock()
	err := writeConfigWithLock(tx, replacer, true)
	if err != nil && !configMutationCommitted(err) && tx.baseline != nil {
		applyConfigData(tx.config, before)
	}
	return err
}

// writeConfigWithLock performs one complete cross-process write transaction.
// The process-local mutex must be held by the caller.
func writeConfigWithLock(tx *configTransaction, replacer func(src, dst string) error, merge bool) (returnErr error) {
	config := tx.config
	configFileDir, err := configFileDirFunc()
	if err != nil {
		return err
	}

	if err := os.MkdirAll(configFileDir, 0700); err != nil {
		return err
	}
	if err := os.Chmod(configFileDir, 0700); err != nil {
		return err
	}

	targetPath := filepath.Join(configFileDir, ConfigFile)
	lock, err := acquireConfigFileLock(targetPath + ".lock")
	if err != nil {
		return fmt.Errorf("lock config file: %w", err)
	}
	defer func() {
		if err := lock.release(); returnErr == nil && err != nil {
			returnErr = &PartialCommitError{Err: fmt.Errorf("release config file lock after committing config: %w", err)}
		}
	}()

	// The disk read, three-way conflict check, and atomic replacement all run
	// under the same cross-process lock. Holding a lock only around replace
	// would still let two processes write from the same stale snapshot.
	// Preserve the exported whole-file writer's historical serialization
	// contract, including nil maps being encoded as null. Transactional writes
	// use normalized copies because mergeConfig operates on map values.
	configToWrite := config
	if merge {
		if tx.baseline == nil || !sameConfigPath(tx.baselinePath, targetPath) {
			return nilBaselineConfigConflictError()
		}
		diskConfig, diskErr := readConfigFileAtPath(targetPath)
		if diskErr != nil {
			return diskErr
		}
		if diskConfig == nil {
			diskConfig = newEmptyConfig()
		}
		configToWrite, err = mergeConfig(tx.baseline, config, diskConfig)
		if err != nil {
			return err
		}
	}

	dir := filepath.Dir(targetPath)
	tempFile, err := os.CreateTemp(dir, ".tmp-config-*")
	if err != nil {
		return err
	}
	tempName := tempFile.Name()
	defer func() {
		_ = tempFile.Close()
		_ = os.Remove(tempName)
	}()
	if err := tempFile.Chmod(0600); err != nil {
		return err
	}

	data, err := marshalConfig(configToWrite)
	if err != nil {
		return err
	}
	if _, err := tempFile.Write(data); err != nil {
		return err
	}
	if err := tempFile.Sync(); err != nil {
		return err
	}
	if err := tempFile.Close(); err != nil {
		return err
	}

	replaceErr := replacer(tempName, targetPath)
	if replaceErr != nil {
		var partial *PartialCommitError
		if !errors.As(replaceErr, &partial) || !partial.Committed() {
			return replaceErr
		}
	}
	if merge {
		// Transactional CLI writes must advance both the working value and its
		// baseline to the exact merged value committed on disk. The exported
		// whole-file writer historically did not mutate its caller-owned value,
		// including the identity of nested maps and pointers.
		applyConfigData(config, configToWrite)
		setConfigBaseline(tx, configToWrite, targetPath)
	}
	return replaceErr
}

func newEmptyConfig() *Configure {
	return &Configure{
		Profiles:   make(map[string]*Profile),
		SsoSession: make(map[string]*SsoSession),
	}
}

func normalizeConfig(cfg *Configure) {
	if cfg == nil {
		return
	}
	if cfg.Profiles == nil {
		cfg.Profiles = make(map[string]*Profile)
	}
	if cfg.SsoSession == nil {
		cfg.SsoSession = make(map[string]*SsoSession)
	}
}

func normalizedConfigCopy(cfg *Configure) *Configure {
	cloned := cloneConfigData(cfg)
	if cloned == nil {
		cloned = newEmptyConfig()
	}
	normalizeConfig(cloned)
	return cloned
}

func cloneConfigData(cfg *Configure) *Configure {
	if cfg == nil {
		return nil
	}
	cloned := &Configure{
		Current:     cfg.Current,
		EnableColor: cfg.EnableColor,
		Profiles:    make(map[string]*Profile, len(cfg.Profiles)),
		SsoSession:  make(map[string]*SsoSession, len(cfg.SsoSession)),
	}
	for name, profile := range cfg.Profiles {
		cloned.Profiles[name] = cloneProfile(profile)
	}
	for name, session := range cfg.SsoSession {
		cloned.SsoSession[name] = cloneSsoSession(session)
	}
	return cloned
}

func cloneSsoSession(session *SsoSession) *SsoSession {
	if session == nil {
		return nil
	}
	cloned := *session
	cloned.RegistrationScopes = append([]string(nil), session.RegistrationScopes...)
	return &cloned
}

func applyConfigData(dst, src *Configure) {
	cloned := normalizedConfigCopy(src)
	dst.Current = cloned.Current
	dst.Profiles = cloned.Profiles
	dst.EnableColor = cloned.EnableColor
	dst.SsoSession = cloned.SsoSession
}

func newConfigTransaction(cfg, snapshot *Configure, path string) *configTransaction {
	tx := &configTransaction{config: cfg}
	setConfigBaseline(tx, snapshot, path)
	return tx
}

func setConfigBaseline(tx *configTransaction, snapshot *Configure, path string) {
	if tx == nil {
		return
	}
	tx.baseline = normalizedConfigCopy(snapshot)
	tx.baselinePath = filepath.Clean(path)
}

func sameConfigPath(left, right string) bool {
	return left != "" && filepath.Clean(left) == filepath.Clean(right)
}

func configDataEmpty(cfg *Configure) bool {
	cfg = normalizedConfigCopy(cfg)
	return cfg.Current == "" && !cfg.EnableColor && len(cfg.Profiles) == 0 && len(cfg.SsoSession) == 0
}

func configDataEqual(left, right *Configure) bool {
	return reflect.DeepEqual(normalizedConfigCopy(left), normalizedConfigCopy(right))
}

func mergeConfig(base, local, remote *Configure) (*Configure, error) {
	base = normalizedConfigCopy(base)
	local = normalizedConfigCopy(local)
	remote = normalizedConfigCopy(remote)
	merged := normalizedConfigCopy(remote)

	localCurrentChanged := local.Current != base.Current
	remoteCurrentChanged := remote.Current != base.Current
	if localCurrentChanged && remoteCurrentChanged && local.Current != remote.Current {
		return nil, configConflictError("current")
	}
	if localCurrentChanged {
		merged.Current = local.Current
	}

	localColorChanged := local.EnableColor != base.EnableColor
	remoteColorChanged := remote.EnableColor != base.EnableColor
	if localColorChanged && remoteColorChanged && local.EnableColor != remote.EnableColor {
		return nil, configConflictError("enableColor")
	}
	if localColorChanged {
		merged.EnableColor = local.EnableColor
	}

	profiles, err := mergeProfileMaps(base.Profiles, local.Profiles, remote.Profiles)
	if err != nil {
		return nil, err
	}
	merged.Profiles = profiles
	if merged.Current != "" {
		_, mergedHasCurrent := merged.Profiles[merged.Current]
		_, baseHadCurrent := base.Profiles[base.Current]
		preexistingDanglingCurrent := merged.Current == base.Current && !baseHadCurrent
		if !mergedHasCurrent && !preexistingDanglingCurrent {
			return nil, configConflictError("current/profile relationship")
		}
	}
	sessions, err := mergeSsoSessionMaps(base.SsoSession, local.SsoSession, remote.SsoSession)
	if err != nil {
		return nil, err
	}
	merged.SsoSession = sessions
	return merged, nil
}

func mergeProfileMaps(base, local, remote map[string]*Profile) (map[string]*Profile, error) {
	keys := sortedProfileKeys(base, local, remote)
	merged := make(map[string]*Profile, len(keys))
	for _, key := range keys {
		baseValue, baseOK := base[key]
		localValue, localOK := local[key]
		remoteValue, remoteOK := remote[key]
		localChanged := !profileEntryEqual(baseValue, baseOK, localValue, localOK)
		remoteChanged := !profileEntryEqual(baseValue, baseOK, remoteValue, remoteOK)
		if localChanged && remoteChanged && !profileEntryEqual(localValue, localOK, remoteValue, remoteOK) {
			return nil, configConflictError(fmt.Sprintf("profiles[%q]", key))
		}
		chosen, exists := remoteValue, remoteOK
		if localChanged {
			chosen, exists = localValue, localOK
		}
		if exists {
			merged[key] = cloneProfile(chosen)
		}
	}
	return merged, nil
}

func mergeSsoSessionMaps(base, local, remote map[string]*SsoSession) (map[string]*SsoSession, error) {
	keys := sortedSsoSessionKeys(base, local, remote)
	merged := make(map[string]*SsoSession, len(keys))
	for _, key := range keys {
		baseValue, baseOK := base[key]
		localValue, localOK := local[key]
		remoteValue, remoteOK := remote[key]
		localChanged := !ssoSessionEntryEqual(baseValue, baseOK, localValue, localOK)
		remoteChanged := !ssoSessionEntryEqual(baseValue, baseOK, remoteValue, remoteOK)
		if localChanged && remoteChanged && !ssoSessionEntryEqual(localValue, localOK, remoteValue, remoteOK) {
			return nil, configConflictError(fmt.Sprintf("sso-session[%q]", key))
		}
		chosen, exists := remoteValue, remoteOK
		if localChanged {
			chosen, exists = localValue, localOK
		}
		if exists {
			merged[key] = cloneSsoSession(chosen)
		}
	}
	return merged, nil
}

func profileEntryEqual(left *Profile, leftOK bool, right *Profile, rightOK bool) bool {
	return leftOK == rightOK && (!leftOK || reflect.DeepEqual(left, right))
}

func ssoSessionEntryEqual(left *SsoSession, leftOK bool, right *SsoSession, rightOK bool) bool {
	return leftOK == rightOK && (!leftOK || reflect.DeepEqual(left, right))
}

func sortedProfileKeys(maps ...map[string]*Profile) []string {
	keys := make(map[string]struct{})
	for _, values := range maps {
		for key := range values {
			keys[key] = struct{}{}
		}
	}
	return sortedConfigKeys(keys)
}

func sortedSsoSessionKeys(maps ...map[string]*SsoSession) []string {
	keys := make(map[string]struct{})
	for _, values := range maps {
		for key := range values {
			keys[key] = struct{}{}
		}
	}
	return sortedConfigKeys(keys)
}

func sortedConfigKeys(keys map[string]struct{}) []string {
	result := make([]string, 0, len(keys))
	for key := range keys {
		result = append(result, key)
	}
	sort.Strings(result)
	return result
}

func configConflictError(field string) error {
	return fmt.Errorf("%w: config field %s was changed by another process", ErrConcurrentConfigModification, field)
}

func nilBaselineConfigConflictError() error {
	return fmt.Errorf("%w: cannot safely overwrite existing config without a baseline; reload the config and retry", ErrConcurrentConfigModification)
}

func marshalConfig(config *Configure) ([]byte, error) {
	data, err := json.MarshalIndent(config, "", "    ")
	if err != nil {
		return nil, err
	}
	return append(data, '\n'), nil
}

func (config *Configure) SetRandomCurrentProfile() {
	if config == nil {
		return
	}

	if config.Profiles == nil || len(config.Profiles) == 0 {
		config.Current = ""
		return
	}

	config.Current = ""
	for key := range config.Profiles {
		if config.Current == "" {
			config.Current = key
			break
		}
	}
}

func setConfigProfile(profile *Profile) error {
	var (
		exist          bool
		currentProfile *Profile
	)

	tx, err := configForWrite()
	if err != nil {
		return err
	}
	cfg := tx.config
	if cfg.Profiles == nil {
		cfg.Profiles = make(map[string]*Profile)
	}

	// check if the target profileFlags already exists
	// otherwise create a new profileFlags
	if currentProfile, exist = cfg.Profiles[profile.Name]; !exist {
		currentProfile = &Profile{
			Name:         profile.Name,
			Mode:         ModeAK,
			DisableSSL:   new(bool),
			UseDualStack: new(bool),
		}
		*currentProfile.DisableSSL = false
		*currentProfile.UseDualStack = false
	}

	nextProfile := mergeProfile(currentProfile, profile)
	if err := validateProfileMode(nextProfile); err != nil {
		return err
	}

	cfg.Profiles[nextProfile.Name] = nextProfile
	cfg.Current = nextProfile.Name
	// 写入配置文件，完成持久化。
	return writeConfigTransaction(tx)
}

func mergeProfile(base *Profile, input *Profile) *Profile {
	merged := cloneProfile(base)
	if merged == nil {
		merged = &Profile{}
	}

	if input == nil {
		return merged
	}

	if input.Name != "" {
		merged.Name = input.Name
	}
	if input.AccessKey != "" {
		merged.AccessKey = input.AccessKey
	}
	if input.SecretKey != "" {
		merged.SecretKey = input.SecretKey
	}
	if input.Region != "" {
		merged.Region = input.Region
	}
	if input.Endpoint != "" {
		merged.Endpoint = input.Endpoint
	}
	if input.EndpointResolver != "" {
		merged.EndpointResolver = input.EndpointResolver
	}
	if input.HTTPProxy != "" {
		merged.HTTPProxy = input.HTTPProxy
	}
	if input.HTTPSProxy != "" {
		merged.HTTPSProxy = input.HTTPSProxy
	}
	if input.SessionToken != "" {
		merged.SessionToken = input.SessionToken
	}
	if input.DisableSSL != nil {
		if merged.DisableSSL == nil {
			merged.DisableSSL = new(bool)
		}
		*merged.DisableSSL = *input.DisableSSL
	}
	if input.UseDualStack != nil {
		if merged.UseDualStack == nil {
			merged.UseDualStack = new(bool)
		}
		*merged.UseDualStack = *input.UseDualStack
	}
	if input.SsoSessionName != "" {
		merged.SsoSessionName = input.SsoSessionName
	}
	if input.AccountId != "" {
		merged.AccountId = input.AccountId
	}
	if input.RoleName != "" {
		merged.RoleName = input.RoleName
	}
	if input.OidcTokenFile != "" {
		merged.OidcTokenFile = input.OidcTokenFile
	}
	if input.RoleTrn != "" {
		merged.RoleTrn = input.RoleTrn
	}
	if input.Mode != "" {
		merged.Mode = input.Mode
	}
	// 仅新建 profile 时默认 mode 为 ak，修改已有 profile 时保留原 mode
	if base == nil && merged.Mode == "" {
		merged.Mode = ModeAK
	}

	return merged
}

func cloneProfile(p *Profile) *Profile {
	if p == nil {
		return nil
	}

	clone := *p
	if p.DisableSSL != nil {
		clone.DisableSSL = new(bool)
		*clone.DisableSSL = *p.DisableSSL
	}
	if p.UseDualStack != nil {
		clone.UseDualStack = new(bool)
		*clone.UseDualStack = *p.UseDualStack
	}
	return &clone
}

func getConfigProfile(profileName string) error {
	var (
		exist          bool
		currentProfile *Profile
		cfg            *Configure
	)

	// 若配置为空则初始化基础结构。
	if cfg = ctx.config; cfg == nil {
		fmt.Println(tr("no profile created"))
		return nil
	}

	if profileName == "" {
		fmt.Printf(tr("no profile name specified, show current profile: [%v]\n"), cfg.Current)
		profileName = cfg.Current
	}

	// check if the target profile already exists, otherwise print an empty profileFlags
	if currentProfile, exist = cfg.Profiles[profileName]; !exist || currentProfile == nil {
		currentProfile = &Profile{}
	}

	util.ShowJson(currentProfile.ToMap(), shouldColorJSON(cfg, os.Stdout))
	return nil
}

func listConfigProfiles() error {
	var (
		cfg *Configure
	)

	// 若配置为空则初始化基础结构。
	if cfg = ctx.config; cfg == nil {
		fmt.Println(tr("no profile created"))
		return nil
	}

	fmt.Printf(tr("*** current profile: %v ***\n"), ctx.config.Current)
	for _, profile := range ctx.config.Profiles {
		util.ShowJson(profile.ToMap(), shouldColorJSON(cfg, os.Stdout))
	}
	return nil
}

func deleteConfigProfile(profileName string) error {
	var (
		exist bool
		cfg   *Configure
	)

	// 若配置为空则初始化基础结构。
	if cfg = ctx.config; cfg == nil {
		return trErrorf("configuration profile %v not found", profileName)
	}
	tx, err := prepareConfigForMutation(cfg)
	if err != nil {
		return err
	}

	// check if the target profileFlags exists
	if _, exist = cfg.Profiles[profileName]; !exist {
		return trErrorf("configuration profile %v not found", profileName)
	}

	// delete profileFlags and write change to config file
	delete(cfg.Profiles, profileName)
	if profileName == cfg.Current {
		cfg.SetRandomCurrentProfile()
		fmt.Printf(tr("delete current profile, set new current profile to [%v]\n"), cfg.Current)
	}

	// 写入配置文件，完成持久化。
	return writeConfigTransaction(tx)
}

func changeConfigProfile(profileName string) error {
	var (
		exist bool
		cfg   *Configure
	)

	// 若配置为空则初始化基础结构。
	if cfg = ctx.config; cfg == nil {
		return trErrorf("configuration profile %v not found", profileName)
	}
	tx, err := prepareConfigForMutation(cfg)
	if err != nil {
		return err
	}

	// check if the target profileFlags exists
	if _, exist = cfg.Profiles[profileName]; !exist {
		return trErrorf("configuration profile %v not found", profileName)
	}

	// if not change,skip it
	if profileName == cfg.Current {
		return nil
	}

	// change current
	cfg.Current = profileName
	// 写入配置文件，完成持久化。
	return writeConfigTransaction(tx)
}

func (p *Profile) ToMap() map[string]interface{} {
	data, _ := json.Marshal(p)
	m := make(map[string]interface{})
	json.Unmarshal(data, &m)

	return m
}

func (p *Profile) String() string {
	b, _ := json.MarshalIndent(p, "", "    ")
	return string(b)
}

// setSsoSession 保存/更新 SSO 会话配置。
// 该函数会规范化 scopes，初始化配置结构，并将会话写入配置文件。
func setSsoSession(session *SsoSession) error {
	var (
		cfg *Configure
	)
	scopes, err := normalizeRegistrationScopes(session.RegistrationScopes)
	if err != nil {
		return err
	}

	tx, err := configForWrite()
	if err != nil {
		return err
	}
	cfg = tx.config

	// 确保 SsoSession 映射已初始化。
	if cfg.SsoSession == nil {
		cfg.SsoSession = make(map[string]*SsoSession)
	}

	// 构建新会话对象，使用规范化后的 scopes。
	newSession := &SsoSession{
		Name:               session.Name,
		StartURL:           session.StartURL,
		Region:             session.Region,
		RegistrationScopes: scopes,
	}

	// 写入内存配置并提示成功。
	cfg.SsoSession[session.Name] = newSession

	// 写入配置文件，完成持久化。
	return writeConfigTransaction(tx)
}
