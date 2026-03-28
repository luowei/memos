package v1

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/pkg/errors"
	"github.com/usememos/memos/store"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"gopkg.in/yaml.v3"
)

const (
	exportTitleSnippetLimit       = 16
	exportDescriptionSnippetLimit = 160
)

type jekyllFrontMatter struct {
	Layout      string   `yaml:"layout"`
	Title       string   `yaml:"title"`
	Date        string   `yaml:"date"`
	Description string   `yaml:"description"`
	Categories  string   `yaml:"categories"`
	Tags        []string `yaml:"tags"`
	Visibility  string   `yaml:"visibility"`
	Comments    bool     `yaml:"comments"`
}

type exportMemosRequest struct {
	OutputDirectory string `json:"outputDirectory"`
}

type exportMemosResponse struct {
	OutputDirectory string `json:"outputDirectory"`
	ExportedCount   int32  `json:"exportedCount"`
}

func (s *APIV1Service) exportMemos(ctx context.Context, request *exportMemosRequest) (*exportMemosResponse, error) {
	user, err := s.fetchCurrentUser(ctx)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to get user")
	}
	if user == nil {
		return nil, status.Errorf(codes.Unauthenticated, "user not authenticated")
	}

	outputDirectory, err := s.resolveMemoExportDirectory(request.OutputDirectory)
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "invalid output directory: %v", err)
	}
	if err := os.MkdirAll(outputDirectory, 0o755); err != nil {
		return nil, status.Errorf(codes.Internal, "failed to prepare output directory: %v", err)
	}

	memos, err := s.listExportableMemos(ctx, user.ID)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to list memos: %v", err)
	}

	for _, memo := range memos {
		filename, markdown, err := s.buildMemoExport(ctx, memo)
		if err != nil {
			return nil, status.Errorf(codes.Internal, "failed to export memo %s: %v", memo.UID, err)
		}
		fullpath := filepath.Join(outputDirectory, filename)
		if err := os.WriteFile(fullpath, []byte(markdown), 0o644); err != nil {
			return nil, status.Errorf(codes.Internal, "failed to write exported memo %s: %v", memo.UID, err)
		}
	}

	return &exportMemosResponse{
		OutputDirectory: outputDirectory,
		ExportedCount:   int32(len(memos)),
	}, nil
}

func (s *APIV1Service) listExportableMemos(ctx context.Context, userID int32) ([]*store.Memo, error) {
	normalState := store.Normal
	archivedState := store.Archived
	baseFind := &store.FindMemo{
		CreatorID:       &userID,
		ExcludeComments: true,
	}

	normalFind := *baseFind
	normalFind.RowStatus = &normalState
	normalMemos, err := s.Store.ListMemos(ctx, &normalFind)
	if err != nil {
		return nil, errors.Wrap(err, "failed to list normal memos")
	}

	archivedFind := *baseFind
	archivedFind.RowStatus = &archivedState
	archivedMemos, err := s.Store.ListMemos(ctx, &archivedFind)
	if err != nil {
		return nil, errors.Wrap(err, "failed to list archived memos")
	}

	memos := append(normalMemos, archivedMemos...)
	sort.Slice(memos, func(i, j int) bool {
		left := s.getMemoDisplayTimeForExport(memos[i])
		right := s.getMemoDisplayTimeForExport(memos[j])
		if left.Equal(right) {
			return memos[i].UID < memos[j].UID
		}
		return left.Before(right)
	})

	return memos, nil
}

func (s *APIV1Service) buildMemoExport(ctx context.Context, memo *store.Memo) (string, string, error) {
	exportTime := s.getMemoDisplayTimeForExport(memo)
	dateString := exportTime.Format("2006-01-02")

	title := extractMemoExportTitle(memo)
	if title == "" {
		snippet, err := s.getMemoContentSnippet(memo.Content)
		if err != nil {
			return "", "", errors.Wrap(err, "failed to generate fallback title")
		}
		title = buildNormalizedTitle(snippet, exportTitleSnippetLimit)
	}
	if title == "" {
		title = "memo"
	}

	description, err := s.getMemoContentSnippetWithLimit(memo.Content, exportDescriptionSnippetLimit)
	if err != nil {
		return "", "", errors.Wrap(err, "failed to generate description")
	}

	tags := []string{}
	if memo.Payload != nil && len(memo.Payload.Tags) > 0 {
		tags = append(tags, memo.Payload.Tags...)
	}

	category := ""
	if len(tags) > 0 {
		category = tags[0]
	}

	frontMatterBytes, err := yaml.Marshal(jekyllFrontMatter{
		Layout:      "post",
		Title:       title,
		Date:        dateString,
		Description: description,
		Categories:  category,
		Tags:        tags,
		Visibility:  "private",
		Comments:    false,
	})
	if err != nil {
		return "", "", errors.Wrap(err, "failed to marshal front matter")
	}

	filenameTitle := normalizeForPathComponent(title)
	if filenameTitle == "" {
		filenameTitle = "memo"
	}
	filename := fmt.Sprintf("%s-%s-%s.md", dateString, filenameTitle, memo.UID)

	var builder strings.Builder
	builder.WriteString("---\n")
	builder.Write(frontMatterBytes)
	builder.WriteString("---\n\n")
	builder.WriteString(memo.Content)
	if memo.Content == "" || !strings.HasSuffix(memo.Content, "\n") {
		builder.WriteString("\n")
	}

	return filename, builder.String(), nil
}

func (s *APIV1Service) getMemoDisplayTimeForExport(memo *store.Memo) time.Time {
	displayTs := memo.CreatedTs
	if setting, err := s.Store.GetInstanceMemoRelatedSetting(context.Background()); err == nil && setting.DisplayWithUpdateTime {
		displayTs = memo.UpdatedTs
	}
	return time.Unix(displayTs, 0)
}

func (s *APIV1Service) getMemoContentSnippetWithLimit(content string, limit int) (string, error) {
	snippet, err := s.MarkdownService.GenerateSnippet([]byte(content), limit)
	if err != nil {
		return "", errors.Wrap(err, "failed to generate snippet")
	}
	return strings.TrimSpace(snippet), nil
}

func extractMemoExportTitle(memo *store.Memo) string {
	if memo.Payload == nil || memo.Payload.Property == nil {
		return ""
	}
	return strings.TrimSpace(memo.Payload.Property.Title)
}

func buildNormalizedTitle(content string, limit int) string {
	content = strings.TrimSpace(content)
	if content == "" {
		return ""
	}

	var builder strings.Builder
	count := 0
	for _, r := range content {
		if count >= limit {
			break
		}
		if unicode.IsSpace(r) {
			builder.WriteRune(' ')
			count++
			continue
		}
		if unicode.IsLetter(r) || unicode.IsNumber(r) || r == '-' || r == '_' {
			builder.WriteRune(unicode.ToLower(r))
			count++
		}
	}

	return strings.TrimSpace(strings.Join(strings.Fields(builder.String()), " "))
}

func normalizeForPathComponent(value string) string {
	value = strings.TrimSpace(strings.ToLower(value))
	if value == "" {
		return ""
	}

	var builder strings.Builder
	lastWasDash := false
	for _, r := range value {
		switch {
		case unicode.IsLetter(r) || unicode.IsNumber(r):
			builder.WriteRune(r)
			lastWasDash = false
		case unicode.IsSpace(r) || r == '-' || r == '_':
			if !lastWasDash && builder.Len() > 0 {
				builder.WriteRune('-')
				lastWasDash = true
			}
		}
	}

	result := strings.Trim(builder.String(), "-")
	if result == "" || !utf8.ValidString(result) {
		return ""
	}
	return result
}

func (s *APIV1Service) resolveMemoExportDirectory(outputDirectory string) (string, error) {
	outputDirectory = strings.TrimSpace(outputDirectory)
	if outputDirectory == "" {
		return "", errors.New("output directory is required")
	}

	if filepath.IsAbs(outputDirectory) {
		return filepath.Clean(outputDirectory), nil
	}

	baseDir := filepath.Clean(s.Profile.Data)
	resolved := filepath.Clean(filepath.Join(baseDir, outputDirectory))
	relative, err := filepath.Rel(baseDir, resolved)
	if err != nil {
		return "", errors.Wrap(err, "failed to resolve relative path")
	}
	if relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return "", errors.New("relative output directory must stay within the server data directory")
	}
	return resolved, nil
}
