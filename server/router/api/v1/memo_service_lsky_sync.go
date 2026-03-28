package v1

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"mime"
	"mime/multipart"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/pkg/errors"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/usememos/memos/server/runner/memopayload"
	"github.com/usememos/memos/store"
)

const (
	lskySyncMarkerStart = "<!-- memos-lsky-sync:start -->"
	lskySyncMarkerEnd   = "<!-- memos-lsky-sync:end -->"
	defaultLskyAPIURL   = "https://lsky.wodedata.com/api/v1"
)

type syncMemoAttachmentsToLskyRequest struct {
	BaseURL    string `json:"baseUrl"`
	Token      string `json:"token"`
	StrategyID string `json:"strategyId"`
}

type syncMemoAttachmentsToLskyResponse struct {
	ScannedMemos         int32                 `json:"scannedMemos"`
	MemosWithAttachments int32                 `json:"memosWithAttachments"`
	UpdatedMemos         int32                 `json:"updatedMemos"`
	SkippedMemos         int32                 `json:"skippedMemos"`
	UploadedFiles        int32                 `json:"uploadedFiles"`
	Results              []*memoLskySyncResult `json:"results"`
}

type memoLskySyncResult struct {
	MemoName        string `json:"memoName"`
	Status          string `json:"status"`
	Reason          string `json:"reason,omitempty"`
	AttachmentCount int32  `json:"attachmentCount,omitempty"`
	UploadedCount   int32  `json:"uploadedCount,omitempty"`
}

type lskyUploadResponse struct {
	Status  bool   `json:"status"`
	Message string `json:"message"`
	Data    struct {
		Links struct {
			URL      string `json:"url"`
			Markdown string `json:"markdown"`
		} `json:"links"`
	} `json:"data"`
}

type generatedImage struct {
	Filename string
	Blob     []byte
}

func (s *APIV1Service) syncMemoAttachmentsToLsky(ctx context.Context, request *syncMemoAttachmentsToLskyRequest) (*syncMemoAttachmentsToLskyResponse, error) {
	user, err := s.fetchCurrentUser(ctx)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to get user")
	}
	if user == nil {
		return nil, status.Errorf(codes.Unauthenticated, "user not authenticated")
	}

	baseURL := strings.TrimSpace(request.BaseURL)
	if baseURL == "" {
		baseURL = defaultLskyAPIURL
	}
	token := strings.TrimSpace(request.Token)
	if token == "" {
		return nil, status.Errorf(codes.InvalidArgument, "lsky token is required")
	}
	strategyID, err := parseOptionalStrategyID(request.StrategyID)
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "invalid strategy id: %v", err)
	}

	memos, err := s.listExportableMemos(ctx, user.ID)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to list memos: %v", err)
	}

	memoIDs := make([]int32, 0, len(memos))
	for _, memo := range memos {
		memoIDs = append(memoIDs, memo.ID)
	}

	attachmentMap := map[int32][]*store.Attachment{}
	if len(memoIDs) > 0 {
		attachments, err := s.Store.ListAttachments(ctx, &store.FindAttachment{MemoIDList: memoIDs})
		if err != nil {
			return nil, status.Errorf(codes.Internal, "failed to list attachments: %v", err)
		}
		for _, attachment := range attachments {
			if attachment.MemoID == nil {
				continue
			}
			attachmentMap[*attachment.MemoID] = append(attachmentMap[*attachment.MemoID], attachment)
		}
	}

	response := &syncMemoAttachmentsToLskyResponse{
		ScannedMemos: int32(len(memos)),
		Results:      []*memoLskySyncResult{},
	}

	for _, memo := range memos {
		result := &memoLskySyncResult{MemoName: fmt.Sprintf("%s%s", MemoNamePrefix, memo.UID)}
		attachments := attachmentMap[memo.ID]
		result.AttachmentCount = int32(len(attachments))
		if len(attachments) > 0 {
			response.MemosWithAttachments++
		}

		switch {
		case len(attachments) == 0:
			result.Status = "skipped"
			result.Reason = "no attachments"
			response.SkippedMemos++
		case strings.Contains(memo.Content, lskySyncMarkerStart):
			result.Status = "skipped"
			result.Reason = "already synced"
			response.SkippedMemos++
		case hasUnsupportedAttachment(attachments):
			result.Status = "skipped"
			result.Reason = "contains unsupported attachments"
			response.SkippedMemos++
		default:
			lines, uploadedCount, err := s.syncAttachmentsForMemoToLsky(ctx, memo, attachments, baseURL, token, strategyID)
			if err != nil {
				result.Status = "skipped"
				result.Reason = err.Error()
				response.SkippedMemos++
			} else if len(lines) == 0 {
				result.Status = "skipped"
				result.Reason = "no uploadable attachments"
				response.SkippedMemos++
			} else {
				if err := s.appendLskyLinksToMemo(ctx, memo, lines); err != nil {
					result.Status = "skipped"
					result.Reason = err.Error()
					response.SkippedMemos++
				} else {
					result.Status = "updated"
					result.UploadedCount = int32(uploadedCount)
					response.UpdatedMemos++
					response.UploadedFiles += int32(uploadedCount)
				}
			}
		}

		response.Results = append(response.Results, result)
	}

	slices.SortStableFunc(response.Results, func(left, right *memoLskySyncResult) int {
		leftPriority := memoLskySyncResultPriority(left)
		rightPriority := memoLskySyncResultPriority(right)
		if leftPriority != rightPriority {
			if leftPriority < rightPriority {
				return -1
			}
			return 1
		}
		if left.AttachmentCount != right.AttachmentCount {
			if left.AttachmentCount > right.AttachmentCount {
				return -1
			}
			return 1
		}
		return strings.Compare(left.MemoName, right.MemoName)
	})

	return response, nil
}

func memoLskySyncResultPriority(result *memoLskySyncResult) int {
	switch {
	case result.Status == "updated":
		return 0
	case result.AttachmentCount > 0:
		return 1
	default:
		return 2
	}
}

func hasUnsupportedAttachment(attachments []*store.Attachment) bool {
	for _, attachment := range attachments {
		if isImageAttachment(attachment) || isPDFAttachment(attachment) {
			continue
		}
		return true
	}
	return false
}

func isImageAttachment(attachment *store.Attachment) bool {
	return strings.HasPrefix(strings.ToLower(normalizeAttachmentMimeType(attachment)), "image/")
}

func isPDFAttachment(attachment *store.Attachment) bool {
	if attachment == nil {
		return false
	}
	if strings.EqualFold(normalizeAttachmentMimeType(attachment), "application/pdf") {
		return true
	}
	return strings.EqualFold(filepath.Ext(attachment.Filename), ".pdf")
}

func (s *APIV1Service) syncAttachmentsForMemoToLsky(ctx context.Context, memo *store.Memo, attachments []*store.Attachment, baseURL, token, strategyID string) ([]string, int, error) {
	lines := []string{}
	uploadedCount := 0

	for _, attachment := range attachments {
		fullAttachment, err := s.Store.GetAttachment(ctx, &store.FindAttachment{ID: &attachment.ID, GetBlob: true})
		if err != nil {
			return nil, 0, errors.Wrap(err, "failed to load attachment")
		}
		if fullAttachment == nil {
			return nil, 0, errors.New("attachment not found")
		}

		blob, err := s.GetAttachmentBlob(fullAttachment)
		if err != nil {
			return nil, 0, errors.Wrap(err, "failed to read attachment blob")
		}

		switch {
		case isImageAttachment(fullAttachment):
			url, err := s.uploadImageToLsky(ctx, baseURL, token, strategyID, fullAttachment.Filename, blob)
			if err != nil {
				return nil, 0, errors.Wrap(err, "failed to upload image attachment")
			}
			lines = append(lines, buildLskyMarkdownImageLine(fullAttachment.Filename, url))
			uploadedCount++
		case isPDFAttachment(fullAttachment):
			previewImages, err := convertPDFToPreviewImages(ctx, fullAttachment.Filename, blob)
			if err != nil {
				return nil, 0, errors.Wrap(err, "failed to convert pdf attachment")
			}
			if len(previewImages) == 0 {
				return nil, 0, errors.New("pdf conversion produced no images")
			}
			for _, previewImage := range previewImages {
				url, err := s.uploadImageToLsky(ctx, baseURL, token, strategyID, previewImage.Filename, previewImage.Blob)
				if err != nil {
					return nil, 0, errors.Wrap(err, "failed to upload converted pdf preview")
				}
				lines = append(lines, buildLskyMarkdownImageLine(previewImage.Filename, url))
				uploadedCount++
			}
		default:
			return nil, 0, errors.Errorf("unsupported attachment type: %s", fullAttachment.Type)
		}
	}

	_ = memo
	return lines, uploadedCount, nil
}

func buildLskyMarkdownImageLine(filename, url string) string {
	altText := strings.TrimSpace(strings.ReplaceAll(strings.ReplaceAll(filename, "[", "("), "]", ")"))
	if altText == "" {
		altText = "image"
	}
	return fmt.Sprintf("![%s](%s)", altText, url)
}

func (s *APIV1Service) appendLskyLinksToMemo(ctx context.Context, memo *store.Memo, lines []string) error {
	if len(lines) == 0 {
		return nil
	}

	content := strings.TrimRight(memo.Content, "\n")
	section := fmt.Sprintf("%s\n%s\n%s", lskySyncMarkerStart, strings.Join(lines, "\n"), lskySyncMarkerEnd)
	if content == "" {
		content = section
	} else {
		content = content + "\n\n" + section
	}

	memo.Content = content
	if err := memopayload.RebuildMemoPayload(memo, s.MarkdownService); err != nil {
		return errors.Wrap(err, "failed to rebuild memo payload")
	}

	updatedTs := time.Now().Unix()
	if err := s.Store.UpdateMemo(ctx, &store.UpdateMemo{
		ID:        memo.ID,
		UpdatedTs: &updatedTs,
		Content:   &memo.Content,
		Payload:   memo.Payload,
	}); err != nil {
		return errors.Wrap(err, "failed to update memo")
	}
	if err := s.syncMemoExportUpdatedTs(ctx, memo.ID, updatedTs); err != nil {
		return err
	}

	return nil
}

func (_ *APIV1Service) uploadImageToLsky(ctx context.Context, baseURL, token, strategyID, filename string, blob []byte) (string, error) {
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)

	fileWriter, err := writer.CreateFormFile("file", filename)
	if err != nil {
		return "", errors.Wrap(err, "failed to create multipart file")
	}
	if _, err := fileWriter.Write(blob); err != nil {
		return "", errors.Wrap(err, "failed to write multipart file content")
	}

	if strings.TrimSpace(strategyID) != "" {
		if err := writer.WriteField("strategy_id", strings.TrimSpace(strategyID)); err != nil {
			return "", errors.Wrap(err, "failed to write strategy id")
		}
	}
	if err := writer.Close(); err != nil {
		return "", errors.Wrap(err, "failed to finalize multipart body")
	}

	uploadURL := strings.TrimRight(baseURL, "/") + "/upload"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, uploadURL, body)
	if err != nil {
		return "", errors.Wrap(err, "failed to create upload request")
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", writer.FormDataContentType())

	client := &http.Client{Timeout: 2 * time.Minute}
	resp, err := client.Do(req)
	if err != nil {
		return "", errors.Wrap(err, "failed to send upload request")
	}
	defer resp.Body.Close()

	var uploadResp lskyUploadResponse
	if err := json.NewDecoder(resp.Body).Decode(&uploadResp); err != nil {
		return "", errors.Wrap(err, "failed to decode upload response")
	}
	if resp.StatusCode != http.StatusOK || !uploadResp.Status {
		if uploadResp.Message == "" {
			uploadResp.Message = resp.Status
		}
		return "", errors.Errorf("lsky upload failed: %s", uploadResp.Message)
	}
	if uploadResp.Data.Links.URL == "" {
		return "", errors.New("lsky upload response did not include an image url")
	}

	return uploadResp.Data.Links.URL, nil
}

func convertPDFToPreviewImages(ctx context.Context, filename string, blob []byte) ([]generatedImage, error) {
	if image, err := convertPDFWithMagick(ctx, filename, blob); err == nil && len(image) > 0 {
		return image, nil
	}
	if runtime.GOOS == "darwin" {
		if image, err := convertPDFWithQuickLook(ctx, filename, blob); err == nil && len(image) > 0 {
			return image, nil
		}
	}
	return nil, errors.New("no supported local pdf-to-image tool is available")
}

func convertPDFWithMagick(ctx context.Context, filename string, blob []byte) ([]generatedImage, error) {
	magickPath, err := exec.LookPath("magick")
	if err != nil {
		return nil, err
	}

	tempDir, err := os.MkdirTemp("", "memos-lsky-magick-*")
	if err != nil {
		return nil, errors.Wrap(err, "failed to create temp dir")
	}
	defer os.RemoveAll(tempDir)

	inputPath := filepath.Join(tempDir, "input.pdf")
	outputPath := filepath.Join(tempDir, "preview.png")
	if err := os.WriteFile(inputPath, blob, 0600); err != nil {
		return nil, errors.Wrap(err, "failed to write temp pdf")
	}

	cmd := exec.CommandContext(ctx, magickPath, "-density", "150", inputPath+"[0]", outputPath)
	if output, err := cmd.CombinedOutput(); err != nil {
		return nil, errors.Wrap(errors.New(string(output)), "magick conversion failed")
	}

	previewBlob, err := os.ReadFile(outputPath)
	if err != nil {
		return nil, errors.Wrap(err, "failed to read magick preview image")
	}

	return []generatedImage{{
		Filename: strings.TrimSuffix(filename, filepath.Ext(filename)) + "-preview.png",
		Blob:     previewBlob,
	}}, nil
}

func convertPDFWithQuickLook(ctx context.Context, filename string, blob []byte) ([]generatedImage, error) {
	qlmanagePath, err := exec.LookPath("qlmanage")
	if err != nil {
		return nil, err
	}

	tempDir, err := os.MkdirTemp("", "memos-lsky-quicklook-*")
	if err != nil {
		return nil, errors.Wrap(err, "failed to create temp dir")
	}
	defer os.RemoveAll(tempDir)

	inputPath := filepath.Join(tempDir, "input.pdf")
	outputDir := filepath.Join(tempDir, "output")
	if err := os.MkdirAll(outputDir, 0700); err != nil {
		return nil, errors.Wrap(err, "failed to create quicklook output dir")
	}
	if err := os.WriteFile(inputPath, blob, 0600); err != nil {
		return nil, errors.Wrap(err, "failed to write temp pdf")
	}

	cmd := exec.CommandContext(ctx, qlmanagePath, "-t", "-s", "1600", "-o", outputDir, inputPath)
	if output, err := cmd.CombinedOutput(); err != nil {
		return nil, errors.Wrap(errors.New(string(output)), "quicklook conversion failed")
	}

	matches, err := filepath.Glob(filepath.Join(outputDir, "*.png"))
	if err != nil {
		return nil, errors.Wrap(err, "failed to locate quicklook preview")
	}
	if len(matches) == 0 {
		return nil, errors.New("quicklook did not produce a preview image")
	}

	previewBlob, err := os.ReadFile(matches[0])
	if err != nil {
		return nil, errors.Wrap(err, "failed to read quicklook preview image")
	}

	return []generatedImage{{
		Filename: strings.TrimSuffix(filename, filepath.Ext(filename)) + "-preview.png",
		Blob:     previewBlob,
	}}, nil
}

func parseOptionalStrategyID(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", nil
	}
	if _, err := strconv.Atoi(value); err != nil {
		return "", errors.New("strategy id must be an integer")
	}
	return value, nil
}

func normalizeAttachmentMimeType(attachment *store.Attachment) string {
	if attachment == nil {
		return ""
	}
	mediaType, _, err := mime.ParseMediaType(attachment.Type)
	if err == nil && mediaType != "" {
		return mediaType
	}
	return attachment.Type
}
