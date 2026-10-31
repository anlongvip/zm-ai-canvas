package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"mime"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"strings"
)

// The v2 upload endpoint uses the same server-side Bearer credential as task
// submission. Legacy user-configured workflows retain their own upload path.
func uploadRunningHubSystemMedia(ctx context.Context, root string, config providerConfig, media providerMedia) (string, error) {
	raw, mimeType, err := mediaBytes(media)
	if err != nil && isPublicMediaURL(strings.TrimSpace(media.URL)) {
		raw, mimeType, err = getExternalBinary(withProviderRequestKind(ctx, "upload"), strings.TrimSpace(media.URL))
	}
	if err != nil {
		return "", err
	}
	if len(raw) == 0 {
		return "", errors.New("RunningHub 参考素材为空")
	}
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	header := make(textproto.MIMEHeader)
	header.Set("Content-Disposition", mime.FormatMediaType("form-data", map[string]string{"name": "file", "filename": providerMediaFilename(media, mimeType)}))
	header.Set("Content-Type", mimeType)
	part, err := writer.CreatePart(header)
	if err != nil {
		return "", err
	}
	if _, err := part.Write(raw); err != nil {
		return "", err
	}
	if err := writer.Close(); err != nil {
		return "", err
	}
	req, err := http.NewRequestWithContext(withProviderRequestKind(ctx, "upload"), http.MethodPost, root+"/openapi/v2/media/upload/binary", body)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+runningHubAPIKey(config))
	req.Header.Set("Content-Type", writer.FormDataContentType())
	ApplyOutboundHeaders(req, config.Headers)
	data, _, err := doBinary(req)
	if err != nil {
		return "", fmt.Errorf("RunningHub 参考素材上传失败：%w", err)
	}
	var response struct {
		Code *int `json:"code"`
		Data struct {
			FileName string `json:"fileName"`
		} `json:"data"`
	}
	if err := json.Unmarshal(data, &response); err != nil {
		return "", errors.New("RunningHub 上传响应不是有效 JSON")
	}
	if response.Code == nil || *response.Code != 0 || strings.TrimSpace(response.Data.FileName) == "" {
		return "", errors.New("RunningHub 素材上传未成功返回文件名")
	}
	return strings.TrimSpace(response.Data.FileName), nil
}
