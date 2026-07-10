package cmd

import (
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/mail"
	"net/url"
	"os"
	"strconv"
	"strings"

	"github.com/TalentedCo/talented-cli/internal/client"
	"github.com/TalentedCo/talented-cli/internal/config"
	"github.com/TalentedCo/talented-cli/internal/credstore"
	"github.com/TalentedCo/talented-cli/internal/exitcode"
	"github.com/spf13/cobra"
)

//go:embed embedded/talented_skill.md
var talentedSkill string

type rootState struct {
	profile string
	apiURL  string
	token   string
	out     io.Writer
	err     io.Writer
}

func Execute() int {
	cmd := NewRootCmd(os.Stdout, os.Stderr)
	if err := cmd.Execute(); err != nil {
		var exitErr *exitcode.Error
		if errors.As(err, &exitErr) {
			if exitErr.Err != nil {
				fmt.Fprintln(os.Stderr, exitErr.Err)
			}
			return exitErr.Code
		}
		fmt.Fprintln(os.Stderr, err)
		return exitcode.Generic
	}
	return exitcode.Success
}

func NewRootCmd(out, errOut io.Writer) *cobra.Command {
	state := &rootState{out: out, err: errOut}
	root := &cobra.Command{
		Use:           "talented",
		Short:         "JSON-first CLI for the Talented agent API",
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.PersistentFlags().StringVar(&state.profile, "profile", "", "saved profile to use")
	root.PersistentFlags().StringVar(&state.apiURL, "api-url", "", "Talented API base URL")
	root.PersistentFlags().StringVar(&state.token, "token", "", "Talented API token")

	root.AddCommand(authCmd(state))
	root.AddCommand(whoamiCmd(state))
	root.AddCommand(agentContextCmd(state))
	root.AddCommand(companiesCmd(state))
	root.AddCommand(jobsCmd(state))
	root.AddCommand(applicationsCmd(state))
	root.AddCommand(candidatesCmd(state))
	root.AddCommand(skillCmd(state))
	return root
}

func writeJSON(out io.Writer, value any) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	_, err = fmt.Fprintln(out, string(data))
	return err
}

func writeRawJSON(out io.Writer, data []byte) error {
	var pretty any
	if json.Unmarshal(data, &pretty) == nil {
		return writeJSON(out, pretty)
	}
	_, err := out.Write(append(data, '\n'))
	return err
}

type safeAuthProfile struct {
	Name        string `json:"name"`
	APIURL      string `json:"api_url"`
	Storage     string `json:"storage,omitempty"`
	HasToken    bool   `json:"has_token"`
	TokenPrefix string `json:"token_prefix,omitempty"`
}

type safeAuthConfig struct {
	DefaultProfile string                     `json:"default_profile"`
	Profiles       map[string]safeAuthProfile `json:"profiles"`
}

func tokenPrefix(token string) string {
	if token == "" {
		return ""
	}
	return token[:min(len(token), 12)]
}

func safeProfileForOutput(name string, profile config.Profile) safeAuthProfile {
	hasToken := profile.Token != ""
	if profile.Storage == "keychain" {
		if _, err := credstore.Get(name); err == nil {
			hasToken = true
		}
	}

	return safeAuthProfile{
		Name:        firstNonEmpty(profile.Name, name),
		APIURL:      firstNonEmpty(profile.APIURL, config.DefaultAPIURL),
		Storage:     profile.Storage,
		HasToken:    hasToken,
		TokenPrefix: tokenPrefix(profile.Token),
	}
}

func safeConfigForOutput(cfg config.File) safeAuthConfig {
	profiles := make(map[string]safeAuthProfile, len(cfg.Profiles))
	for name, profile := range cfg.Profiles {
		profiles[name] = safeProfileForOutput(name, profile)
	}
	return safeAuthConfig{
		DefaultProfile: cfg.DefaultProfile,
		Profiles:       profiles,
	}
}

func mapHTTPError(err error) error {
	var httpErr *client.HTTPError
	if !errors.As(err, &httpErr) {
		return exitcode.Wrap(exitcode.Network, err)
	}
	switch httpErr.StatusCode {
	case 401:
		return exitcode.Wrap(exitcode.Auth, err)
	case 404:
		return exitcode.Wrap(exitcode.NotFound, err)
	case 400, 402, 422:
		return exitcode.Wrap(exitcode.Validation, err)
	case 409:
		return exitcode.Wrap(exitcode.Conflict, err)
	default:
		if httpErr.StatusCode >= 500 {
			return exitcode.Wrap(exitcode.Server, err)
		}
		return exitcode.Wrap(exitcode.Generic, err)
	}
}

func (s *rootState) resolveClient() (*client.Client, string, error) {
	cfg, err := config.Load()
	if err != nil {
		return nil, "", exitcode.Wrap(exitcode.Generic, err)
	}
	profileName := config.ResolveProfileName(s.profile, cfg)
	profile := cfg.Profiles[profileName]
	apiURL := firstNonEmpty(s.apiURL, os.Getenv("TALENTED_API_URL"), profile.APIURL, config.DefaultAPIURL)
	token := firstNonEmpty(s.token, os.Getenv("TALENTED_API_TOKEN"), profile.Token)
	if token == "" && profile.Storage == "keychain" {
		if stored, err := credstore.Get(profileName); err == nil {
			token = stored
		}
	}
	if token == "" {
		return nil, profileName, exitcode.Wrap(exitcode.Auth, fmt.Errorf("missing Talented API token; run `talented auth save --token tal_...` or set TALENTED_API_TOKEN"))
	}
	return client.New(apiURL, token), profileName, nil
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func apiGET(state *rootState, path string) error {
	c, _, err := state.resolveClient()
	if err != nil {
		return err
	}
	data, err := c.Do("GET", path, nil)
	if err != nil {
		return mapHTTPError(err)
	}
	return writeRawJSON(state.out, data)
}

func apiJSON(state *rootState, method, path string, body map[string]any) error {
	c, _, err := state.resolveClient()
	if err != nil {
		return err
	}
	data, err := c.Do(method, path, body)
	if err != nil {
		return mapHTTPError(err)
	}
	return writeRawJSON(state.out, data)
}

func authCmd(state *rootState) *cobra.Command {
	cmd := &cobra.Command{Use: "auth", Short: "Manage saved API profiles"}
	cmd.AddCommand(authSaveCmd(state), authStatusCmd(state), authListCmd(state), authUseCmd(state), authLogoutCmd(state))
	return cmd
}

func authSaveCmd(state *rootState) *cobra.Command {
	var profileName, apiURL, token, storage string
	var makeDefault bool
	cmd := &cobra.Command{
		Use:   "save",
		Short: "Save an API token profile",
		RunE: func(cmd *cobra.Command, args []string) error {
			profileName = firstNonEmpty(profileName, state.profile, config.DefaultProfile)
			apiURL = firstNonEmpty(apiURL, state.apiURL, os.Getenv("TALENTED_API_URL"), config.DefaultAPIURL)
			token = firstNonEmpty(token, state.token, os.Getenv("TALENTED_API_TOKEN"))
			if !strings.HasPrefix(token, "tal_") {
				return exitcode.Wrap(exitcode.Auth, fmt.Errorf("a tal_ API token is required"))
			}
			actualStorage, err := credstore.Save(profileName, token, storage)
			if err != nil {
				return exitcode.Wrap(exitcode.Generic, err)
			}
			profile := config.Profile{Name: profileName, APIURL: apiURL, Storage: actualStorage}
			if actualStorage == "file" {
				profile.Token = token
			}
			if err := config.UpsertProfile(profile, makeDefault); err != nil {
				return exitcode.Wrap(exitcode.Generic, err)
			}
			return writeJSON(state.out, map[string]any{
				"profile":      profileName,
				"api_url":      apiURL,
				"storage":      actualStorage,
				"token_prefix": tokenPrefix(token),
			})
		},
	}
	cmd.Flags().StringVar(&profileName, "profile", "", "profile name")
	cmd.Flags().StringVar(&apiURL, "api-url", "", "Talented API base URL")
	cmd.Flags().StringVar(&token, "token", "", "tal_ API token")
	cmd.Flags().StringVar(&storage, "storage", "auto", "token storage: auto, keychain, or file")
	cmd.Flags().BoolVar(&makeDefault, "default", true, "set as default profile")
	return cmd
}

func authStatusCmd(state *rootState) *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show the active profile",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load()
			if err != nil {
				return exitcode.Wrap(exitcode.Generic, err)
			}
			name := config.ResolveProfileName(state.profile, cfg)
			profile := cfg.Profiles[name]
			hasKeychainToken := false
			if profile.Storage == "keychain" {
				_, err := credstore.Get(name)
				hasKeychainToken = err == nil
			}
			return writeJSON(state.out, map[string]any{
				"profile":         name,
				"default_profile": cfg.DefaultProfile,
				"api_url":         firstNonEmpty(state.apiURL, os.Getenv("TALENTED_API_URL"), profile.APIURL, config.DefaultAPIURL),
				"has_token":       state.token != "" || os.Getenv("TALENTED_API_TOKEN") != "" || profile.Token != "" || hasKeychainToken,
				"storage":         profile.Storage,
			})
		},
	}
}

func authListCmd(state *rootState) *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List saved profiles",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load()
			if err != nil {
				return exitcode.Wrap(exitcode.Generic, err)
			}
			return writeJSON(state.out, safeConfigForOutput(cfg))
		},
	}
}

func authUseCmd(state *rootState) *cobra.Command {
	return &cobra.Command{
		Use:   "use <profile>",
		Short: "Set the default profile",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load()
			if err != nil {
				return exitcode.Wrap(exitcode.Generic, err)
			}
			if _, ok := cfg.Profiles[args[0]]; !ok {
				return exitcode.Wrap(exitcode.NotFound, fmt.Errorf("profile not found: %s", args[0]))
			}
			cfg.DefaultProfile = args[0]
			if err := config.Save(cfg); err != nil {
				return exitcode.Wrap(exitcode.Generic, err)
			}
			return writeJSON(state.out, map[string]any{"default_profile": args[0]})
		},
	}
}

func authLogoutCmd(state *rootState) *cobra.Command {
	var profileName string
	return &cobra.Command{
		Use:   "logout",
		Short: "Remove a saved profile",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load()
			if err != nil {
				return exitcode.Wrap(exitcode.Generic, err)
			}
			name := firstNonEmpty(profileName, state.profile, cfg.DefaultProfile, config.DefaultProfile)
			delete(cfg.Profiles, name)
			_ = credstore.Delete(name)
			if cfg.DefaultProfile == name {
				cfg.DefaultProfile = config.DefaultProfile
			}
			if err := config.Save(cfg); err != nil {
				return exitcode.Wrap(exitcode.Generic, err)
			}
			return writeJSON(state.out, map[string]any{"removed_profile": name})
		},
	}
}

func whoamiCmd(state *rootState) *cobra.Command {
	return &cobra.Command{
		Use:   "whoami",
		Short: "Show authenticated Talented agent context",
		RunE: func(cmd *cobra.Command, args []string) error {
			return apiGET(state, "/api/agent/v1/me")
		},
	}
}

func agentContextCmd(state *rootState) *cobra.Command {
	return &cobra.Command{
		Use:   "agent-context",
		Short: "Emit machine-readable command and API context",
		RunE: func(cmd *cobra.Command, args []string) error {
			c, profileName, err := state.resolveClient()
			if err != nil {
				return err
			}
			data, err := c.Do("GET", "/api/agent/v1/me", nil)
			if err != nil {
				return mapHTTPError(err)
			}
			var me any
			_ = json.Unmarshal(data, &me)
			return writeJSON(state.out, map[string]any{
				"profile": profileName,
				"api_url": c.BaseURL,
				"context": me,
				"safe_scope": []string{
					"companies", "jobs", "applications", "single application stage moves", "candidate notes", "candidate status/favorite",
					"safe company profile updates",
				},
				"excluded_scope": []string{
					"super-admin", "billing", "impersonation", "raw database", "bulk destructive automation",
				},
				"commands": []string{
					"talented companies list",
					"talented companies update --company <id> --location 'Hubert, NC'",
					"talented companies invite --company <id> --email teammate@example.com --role ADMIN",
					"talented jobs list --company <id>",
					"talented applications list --job <id>",
					"talented applications move --application <id> --stage <id>",
					"talented candidates notes add --candidate <id> --content <text>",
				},
			})
		},
	}
}

func companiesCmd(state *rootState) *cobra.Command {
	cmd := &cobra.Command{Use: "companies", Short: "Company reads, profile updates, and member invites"}
	cmd.AddCommand(&cobra.Command{
		Use:   "list",
		Short: "List accessible companies",
		RunE: func(cmd *cobra.Command, args []string) error {
			return apiGET(state, "/api/agent/v1/companies")
		},
	})
	get := &cobra.Command{
		Use:   "get",
		Short: "Get a company",
		RunE: func(cmd *cobra.Command, args []string) error {
			id, _ := cmd.Flags().GetInt("company")
			if id <= 0 {
				return exitcode.Wrap(exitcode.Usage, fmt.Errorf("--company is required"))
			}
			return apiGET(state, fmt.Sprintf("/api/agent/v1/companies/%d", id))
		},
	}
	get.Flags().Int("company", 0, "company ID")
	cmd.AddCommand(get)
	cmd.AddCommand(companyUpdateCmd(state))
	cmd.AddCommand(companyInviteCmd(state))
	return cmd
}

func companyUpdateCmd(state *rootState) *cobra.Command {
	var company int
	var description, website, logoURL, location, industry, size, timezone string
	cmd := &cobra.Command{
		Use:   "update",
		Short: "Update safe company profile fields; requires owner/admin",
		Long: "Update safe profile fields on an existing company. Omitted flags preserve existing values. " +
			"Passing an empty string clears nullable fields; --timezone must be a non-empty valid IANA timezone. " +
			"This command never creates companies and never exposes raw company mutation.",
		RunE: func(cmd *cobra.Command, args []string) error {
			if company <= 0 {
				return exitcode.Wrap(exitcode.Usage, fmt.Errorf("--company is required"))
			}
			body := changedStringMap(cmd,
				"description", description,
				"website", website,
				"logoUrl", logoURL,
				"location", location,
				"industry", industry,
				"size", size,
				"timezone", timezone,
			)
			if len(body) == 0 {
				return exitcode.Wrap(exitcode.Usage, fmt.Errorf("at least one company profile field flag is required"))
			}
			return apiJSON(state, "PATCH", fmt.Sprintf("/api/agent/v1/companies/%d", company), body)
		},
	}
	cmd.Flags().IntVar(&company, "company", 0, "existing company ID")
	cmd.Flags().StringVar(&description, "description", "", "company description; empty string clears it")
	cmd.Flags().StringVar(&website, "website", "", "company website; bare domains are normalized by the API")
	cmd.Flags().StringVar(&logoURL, "logo-url", "", "company logo image URL; empty string clears it")
	cmd.Flags().StringVar(&location, "location", "", "company location; empty string clears it")
	cmd.Flags().StringVar(&industry, "industry", "", "company industry; empty string clears it")
	cmd.Flags().StringVar(&size, "size", "", "company size/headcount band; empty string clears it")
	cmd.Flags().StringVar(&timezone, "timezone", "", "company IANA timezone, e.g. America/New_York")
	return cmd
}

func companyInviteCmd(state *rootState) *cobra.Command {
	var company int
	var email, role string
	cmd := &cobra.Command{
		Use:   "invite",
		Short: "Invite or add a member/admin to an existing company",
		Long: "Invite or add a member/admin to an existing company. This command never creates companies; " +
			"the API requires agent:write plus company OWNER/ADMIN permissions.",
		RunE: func(cmd *cobra.Command, args []string) error {
			if company <= 0 {
				return exitcode.Wrap(exitcode.Usage, fmt.Errorf("--company is required"))
			}
			normalizedEmail, err := normalizeInviteEmail(email)
			if err != nil {
				return exitcode.Wrap(exitcode.Validation, err)
			}
			normalizedRole := strings.ToUpper(strings.TrimSpace(role))
			if normalizedRole != "ADMIN" && normalizedRole != "MEMBER" {
				return exitcode.Wrap(exitcode.Validation, fmt.Errorf("--role must be ADMIN or MEMBER"))
			}
			return apiJSON(
				state,
				"POST",
				fmt.Sprintf("/api/agent/v1/companies/%d/members/invite", company),
				map[string]any{"email": normalizedEmail, "role": normalizedRole},
			)
		},
	}
	cmd.Flags().IntVar(&company, "company", 0, "existing company ID")
	cmd.Flags().StringVar(&email, "email", "", "email address to invite")
	cmd.Flags().StringVar(&role, "role", "", "role to assign: ADMIN or MEMBER")
	return cmd
}

func normalizeInviteEmail(raw string) (string, error) {
	email := strings.ToLower(strings.TrimSpace(raw))
	if email == "" {
		return "", fmt.Errorf("--email is required")
	}
	address, err := mail.ParseAddress(email)
	if err != nil || address.Address != email {
		return "", fmt.Errorf("--email must be a valid email address")
	}
	return email, nil
}

func jobsCmd(state *rootState) *cobra.Command {
	cmd := &cobra.Command{Use: "jobs", Short: "Job reads and admin writes"}
	cmd.AddCommand(jobsListCmd(state), jobsGetCmd(state), jobsCreateCmd(state), jobsUpdateCmd(state), jobsStatusCmd(state))
	return cmd
}

func jobsListCmd(state *rootState) *cobra.Command {
	var company int
	var status, search string
	var includeArchived bool
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List jobs for a company",
		RunE: func(cmd *cobra.Command, args []string) error {
			if company <= 0 {
				return exitcode.Wrap(exitcode.Usage, fmt.Errorf("--company is required"))
			}
			q := url.Values{}
			if status != "" {
				q.Set("status", status)
			}
			if search != "" {
				q.Set("search", search)
			}
			if includeArchived {
				q.Set("includeArchived", "true")
			}
			path := fmt.Sprintf("/api/agent/v1/companies/%d/jobs", company)
			if encoded := q.Encode(); encoded != "" {
				path += "?" + encoded
			}
			return apiGET(state, path)
		},
	}
	cmd.Flags().IntVar(&company, "company", 0, "company ID")
	cmd.Flags().StringVar(&status, "status", "", "job status filter")
	cmd.Flags().StringVar(&search, "search", "", "search title, department, or location")
	cmd.Flags().BoolVar(&includeArchived, "include-archived", false, "include archived jobs")
	return cmd
}

func jobsGetCmd(state *rootState) *cobra.Command {
	var job int
	cmd := &cobra.Command{
		Use:   "get",
		Short: "Get a job",
		RunE: func(cmd *cobra.Command, args []string) error {
			if job <= 0 {
				return exitcode.Wrap(exitcode.Usage, fmt.Errorf("--job is required"))
			}
			return apiGET(state, fmt.Sprintf("/api/agent/v1/jobs/%d", job))
		},
	}
	cmd.Flags().IntVar(&job, "job", 0, "job ID")
	return cmd
}

func jobsCreateCmd(state *rootState) *cobra.Command {
	var company int
	var title, description, department, level, location, employmentType, remoteOption, salaryRange string
	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create a draft job; requires owner/admin",
		RunE: func(cmd *cobra.Command, args []string) error {
			if company <= 0 || title == "" {
				return exitcode.Wrap(exitcode.Usage, fmt.Errorf("--company and --title are required"))
			}
			body := stringMap(
				"title", title,
				"description", description,
				"department", department,
				"level", level,
				"location", location,
				"employmentType", employmentType,
				"remoteOption", remoteOption,
				"salaryRange", salaryRange,
			)
			return apiJSON(state, "POST", fmt.Sprintf("/api/agent/v1/companies/%d/jobs", company), body)
		},
	}
	cmd.Flags().IntVar(&company, "company", 0, "company ID")
	cmd.Flags().StringVar(&title, "title", "", "job title")
	cmd.Flags().StringVar(&description, "description", "", "job description")
	cmd.Flags().StringVar(&department, "department", "", "department")
	cmd.Flags().StringVar(&level, "level", "", "level")
	cmd.Flags().StringVar(&location, "location", "", "location")
	cmd.Flags().StringVar(&employmentType, "employment-type", "", "employment type")
	cmd.Flags().StringVar(&remoteOption, "remote-option", "", "remote option")
	cmd.Flags().StringVar(&salaryRange, "salary-range", "", "salary range")
	return cmd
}

func jobsUpdateCmd(state *rootState) *cobra.Command {
	var job int
	var title, description, department, level, location, employmentType, remoteOption, salaryRange string
	cmd := &cobra.Command{
		Use:   "update",
		Short: "Update safe job fields; requires owner/admin",
		RunE: func(cmd *cobra.Command, args []string) error {
			if job <= 0 {
				return exitcode.Wrap(exitcode.Usage, fmt.Errorf("--job is required"))
			}
			body := changedStringMap(cmd,
				"title", title,
				"description", description,
				"department", department,
				"level", level,
				"location", location,
				"employmentType", employmentType,
				"remoteOption", remoteOption,
				"salaryRange", salaryRange,
			)
			return apiJSON(state, "PATCH", fmt.Sprintf("/api/agent/v1/jobs/%d", job), body)
		},
	}
	cmd.Flags().IntVar(&job, "job", 0, "job ID")
	cmd.Flags().StringVar(&title, "title", "", "job title")
	cmd.Flags().StringVar(&description, "description", "", "job description")
	cmd.Flags().StringVar(&department, "department", "", "department")
	cmd.Flags().StringVar(&level, "level", "", "level")
	cmd.Flags().StringVar(&location, "location", "", "location")
	cmd.Flags().StringVar(&employmentType, "employment-type", "", "employment type")
	cmd.Flags().StringVar(&remoteOption, "remote-option", "", "remote option")
	cmd.Flags().StringVar(&salaryRange, "salary-range", "", "salary range")
	return cmd
}

func jobsStatusCmd(state *rootState) *cobra.Command {
	var job int
	var status string
	cmd := &cobra.Command{
		Use:   "status",
		Short: "Set job status; requires owner/admin",
		RunE: func(cmd *cobra.Command, args []string) error {
			if job <= 0 || status == "" {
				return exitcode.Wrap(exitcode.Usage, fmt.Errorf("--job and --status are required"))
			}
			return apiJSON(state, "POST", fmt.Sprintf("/api/agent/v1/jobs/%d/status", job), map[string]any{"status": status})
		},
	}
	cmd.Flags().IntVar(&job, "job", 0, "job ID")
	cmd.Flags().StringVar(&status, "status", "", "DRAFT, ACTIVE, PAUSED, COMPLETED, or ARCHIVED")
	return cmd
}

func applicationsCmd(state *rootState) *cobra.Command {
	cmd := &cobra.Command{Use: "applications", Short: "Application reads and pipeline actions"}
	cmd.AddCommand(applicationsListCmd(state), applicationsGetCmd(state), applicationsCreateCmd(state), applicationsMoveCmd(state), applicationsRejectCmd(state), applicationsUnrejectCmd(state))
	return cmd
}

func applicationsListCmd(state *rootState) *cobra.Command {
	var job, limit, offset, stage int
	var status, search string
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List job applications",
		RunE: func(cmd *cobra.Command, args []string) error {
			if job <= 0 {
				return exitcode.Wrap(exitcode.Usage, fmt.Errorf("--job is required"))
			}
			q := url.Values{}
			if limit > 0 {
				q.Set("limit", strconv.Itoa(limit))
			}
			if offset > 0 {
				q.Set("offset", strconv.Itoa(offset))
			}
			if stage > 0 {
				q.Set("stageId", strconv.Itoa(stage))
			}
			if status != "" {
				q.Set("status", status)
			}
			if search != "" {
				q.Set("search", search)
			}
			path := fmt.Sprintf("/api/agent/v1/jobs/%d/applications", job)
			if encoded := q.Encode(); encoded != "" {
				path += "?" + encoded
			}
			return apiGET(state, path)
		},
	}
	cmd.Flags().IntVar(&job, "job", 0, "job ID")
	cmd.Flags().IntVar(&limit, "limit", 50, "page size")
	cmd.Flags().IntVar(&offset, "offset", 0, "page offset")
	cmd.Flags().IntVar(&stage, "stage", 0, "current stage ID")
	cmd.Flags().StringVar(&status, "status", "", "application status")
	cmd.Flags().StringVar(&search, "search", "", "candidate search")
	return cmd
}

func applicationsGetCmd(state *rootState) *cobra.Command {
	var application int
	cmd := &cobra.Command{
		Use:   "get",
		Short: "Get an application",
		RunE: func(cmd *cobra.Command, args []string) error {
			if application <= 0 {
				return exitcode.Wrap(exitcode.Usage, fmt.Errorf("--application is required"))
			}
			return apiGET(state, fmt.Sprintf("/api/agent/v1/applications/%d", application))
		},
	}
	cmd.Flags().IntVar(&application, "application", 0, "application ID")
	return cmd
}

func applicationsCreateCmd(state *rootState) *cobra.Command {
	var job, stage int
	var email, firstName, lastName, phone, linkedInURL, notes, coverLetter, resumeMarkdown string
	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create candidate and application",
		RunE: func(cmd *cobra.Command, args []string) error {
			if job <= 0 || email == "" {
				return exitcode.Wrap(exitcode.Usage, fmt.Errorf("--job and --email are required"))
			}
			body := map[string]any{
				"candidate": stringMap(
					"email", email,
					"firstName", firstName,
					"lastName", lastName,
					"phone", phone,
					"linkedInUrl", linkedInURL,
					"notes", notes,
				),
			}
			if stage > 0 {
				body["stageId"] = stage
			}
			if coverLetter != "" {
				body["coverLetter"] = coverLetter
			}
			if resumeMarkdown != "" {
				body["resumeMarkdown"] = resumeMarkdown
			}
			return apiJSON(state, "POST", fmt.Sprintf("/api/agent/v1/jobs/%d/applications", job), body)
		},
	}
	cmd.Flags().IntVar(&job, "job", 0, "job ID")
	cmd.Flags().IntVar(&stage, "stage", 0, "target stage ID")
	cmd.Flags().StringVar(&email, "email", "", "candidate email")
	cmd.Flags().StringVar(&firstName, "first-name", "", "candidate first name")
	cmd.Flags().StringVar(&lastName, "last-name", "", "candidate last name")
	cmd.Flags().StringVar(&phone, "phone", "", "candidate phone")
	cmd.Flags().StringVar(&linkedInURL, "linkedin-url", "", "LinkedIn URL")
	cmd.Flags().StringVar(&notes, "notes", "", "candidate notes")
	cmd.Flags().StringVar(&coverLetter, "cover-letter", "", "cover letter")
	cmd.Flags().StringVar(&resumeMarkdown, "resume-markdown", "", "resume markdown")
	return cmd
}

func applicationsMoveCmd(state *rootState) *cobra.Command {
	var application, stage int
	cmd := &cobra.Command{
		Use:   "move",
		Short: "Move one application to a valid stage",
		RunE: func(cmd *cobra.Command, args []string) error {
			if application <= 0 || stage <= 0 {
				return exitcode.Wrap(exitcode.Usage, fmt.Errorf("--application and --stage are required"))
			}
			return apiJSON(state, "POST", fmt.Sprintf("/api/agent/v1/applications/%d/stage", application), map[string]any{"stageId": stage})
		},
	}
	cmd.Flags().IntVar(&application, "application", 0, "application ID")
	cmd.Flags().IntVar(&stage, "stage", 0, "stage ID")
	return cmd
}

func applicationsRejectCmd(state *rootState) *cobra.Command {
	var application int
	var reason string
	cmd := &cobra.Command{
		Use:   "reject",
		Short: "Reject one application",
		RunE: func(cmd *cobra.Command, args []string) error {
			if application <= 0 {
				return exitcode.Wrap(exitcode.Usage, fmt.Errorf("--application is required"))
			}
			return apiJSON(state, "POST", fmt.Sprintf("/api/agent/v1/applications/%d/reject", application), stringMap("reason", reason))
		},
	}
	cmd.Flags().IntVar(&application, "application", 0, "application ID")
	cmd.Flags().StringVar(&reason, "reason", "", "rejection reason")
	return cmd
}

func applicationsUnrejectCmd(state *rootState) *cobra.Command {
	var application int
	cmd := &cobra.Command{
		Use:   "unreject",
		Short: "Unreject one application",
		RunE: func(cmd *cobra.Command, args []string) error {
			if application <= 0 {
				return exitcode.Wrap(exitcode.Usage, fmt.Errorf("--application is required"))
			}
			return apiJSON(state, "POST", fmt.Sprintf("/api/agent/v1/applications/%d/unreject", application), map[string]any{})
		},
	}
	cmd.Flags().IntVar(&application, "application", 0, "application ID")
	return cmd
}

func candidatesCmd(state *rootState) *cobra.Command {
	cmd := &cobra.Command{Use: "candidates", Short: "Candidate reads, status, favorite, and notes"}
	cmd.AddCommand(candidatesGetCmd(state), candidatesStatusCmd(state), candidatesFavoriteCmd(state), candidatesNotesCmd(state))
	return cmd
}

func candidatesGetCmd(state *rootState) *cobra.Command {
	var candidate int
	cmd := &cobra.Command{
		Use:   "get",
		Short: "Get a candidate",
		RunE: func(cmd *cobra.Command, args []string) error {
			if candidate <= 0 {
				return exitcode.Wrap(exitcode.Usage, fmt.Errorf("--candidate is required"))
			}
			return apiGET(state, fmt.Sprintf("/api/agent/v1/candidates/%d", candidate))
		},
	}
	cmd.Flags().IntVar(&candidate, "candidate", 0, "candidate ID")
	return cmd
}

func candidatesStatusCmd(state *rootState) *cobra.Command {
	var candidate int
	var status string
	cmd := &cobra.Command{
		Use:   "status",
		Short: "Update candidate status",
		RunE: func(cmd *cobra.Command, args []string) error {
			if candidate <= 0 || status == "" {
				return exitcode.Wrap(exitcode.Usage, fmt.Errorf("--candidate and --status are required"))
			}
			return apiJSON(state, "PATCH", fmt.Sprintf("/api/agent/v1/candidates/%d", candidate), map[string]any{"status": status})
		},
	}
	cmd.Flags().IntVar(&candidate, "candidate", 0, "candidate ID")
	cmd.Flags().StringVar(&status, "status", "", "NEW, CONTACTED, INTERVIEWING, HIRED, or REJECTED")
	return cmd
}

func candidatesFavoriteCmd(state *rootState) *cobra.Command {
	var candidate int
	var favorite bool
	cmd := &cobra.Command{
		Use:   "favorite",
		Short: "Set candidate favorite flag",
		RunE: func(cmd *cobra.Command, args []string) error {
			if candidate <= 0 {
				return exitcode.Wrap(exitcode.Usage, fmt.Errorf("--candidate is required"))
			}
			return apiJSON(state, "PATCH", fmt.Sprintf("/api/agent/v1/candidates/%d", candidate), map[string]any{"isFavorite": favorite})
		},
	}
	cmd.Flags().IntVar(&candidate, "candidate", 0, "candidate ID")
	cmd.Flags().BoolVar(&favorite, "value", true, "favorite value")
	return cmd
}

func candidatesNotesCmd(state *rootState) *cobra.Command {
	var candidate int
	cmd := &cobra.Command{Use: "notes", Short: "Candidate notes"}
	list := &cobra.Command{
		Use:   "list",
		Short: "List candidate notes",
		RunE: func(cmd *cobra.Command, args []string) error {
			if candidate <= 0 {
				return exitcode.Wrap(exitcode.Usage, fmt.Errorf("--candidate is required"))
			}
			return apiGET(state, fmt.Sprintf("/api/agent/v1/candidates/%d/notes", candidate))
		},
	}
	var content string
	add := &cobra.Command{
		Use:   "add",
		Short: "Append a dashboard-visible candidate note",
		RunE: func(cmd *cobra.Command, args []string) error {
			if candidate <= 0 || content == "" {
				return exitcode.Wrap(exitcode.Usage, fmt.Errorf("--candidate and --content are required"))
			}
			return apiJSON(state, "POST", fmt.Sprintf("/api/agent/v1/candidates/%d/notes", candidate), map[string]any{"content": content})
		},
	}
	for _, child := range []*cobra.Command{list, add} {
		child.Flags().IntVar(&candidate, "candidate", 0, "candidate ID")
	}
	add.Flags().StringVar(&content, "content", "", "note content")
	cmd.AddCommand(list, add)
	return cmd
}

func skillCmd(state *rootState) *cobra.Command {
	cmd := &cobra.Command{Use: "skill", Short: "Bundled agent skill"}
	get := &cobra.Command{
		Use:   "get talented",
		Short: "Print the bundled Talented skill",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if args[0] != "talented" {
				return exitcode.Wrap(exitcode.NotFound, fmt.Errorf("unknown skill: %s", args[0]))
			}
			return writeJSON(state.out, map[string]any{
				"name":    "talented",
				"content": talentedSkill,
			})
		},
	}
	cmd.AddCommand(get)
	return cmd
}

func stringMap(values ...string) map[string]any {
	result := map[string]any{}
	for i := 0; i+1 < len(values); i += 2 {
		if values[i+1] != "" {
			result[values[i]] = values[i+1]
		}
	}
	return result
}

func changedStringMap(cmd *cobra.Command, values ...string) map[string]any {
	result := map[string]any{}
	flagByJSONKey := map[string]string{
		"employmentType": "employment-type",
		"logoUrl":        "logo-url",
		"remoteOption":   "remote-option",
		"salaryRange":    "salary-range",
	}
	for i := 0; i+1 < len(values); i += 2 {
		key := values[i]
		flagName := flagByJSONKey[key]
		if flagName == "" {
			flagName = key
		}
		if cmd.Flags().Changed(flagName) {
			result[key] = values[i+1]
		}
	}
	return result
}
