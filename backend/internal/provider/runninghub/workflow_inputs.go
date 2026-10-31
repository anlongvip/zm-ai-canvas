package runninghub

import (
	"fmt"
	"math"
	"strconv"
	"strings"
)

type VideoInputs struct {
	Prompt      string
	Duration    int
	AspectRatio string
	Megapixels  float64
	Images      []string
	Videos      []string
	Audios      []string
}

// BuildVideoInputs maps the common video request into the currently published
// 智墨 H3 RunningHub workflow. The node IDs stay in this provider definition;
// callers only deal with normalized video options.
func BuildVideoInputs(input VideoInputs) ([]NodeInput, error) {
	prompt := strings.TrimSpace(input.Prompt)
	if prompt == "" {
		return nil, fmt.Errorf("视频提示词不能为空")
	}
	if input.Duration < 1 || input.Duration > 20 {
		return nil, fmt.Errorf("视频时长必须在 1 到 20 秒之间")
	}
	if strings.TrimSpace(input.AspectRatio) == "" {
		return nil, fmt.Errorf("视频画幅不能为空")
	}
	ratio := strings.TrimSpace(input.AspectRatio)
	validRatio := false
	for _, option := range []string{"1:1 (Square)", "2:3 (Portrait Photo)", "3:2 (Photo)", "3:4 (Portrait Standard)", "4:3 (Standard)", "9:16 (Portrait Widescreen)", "16:9 (Widescreen)", "21:9 (Ultrawide)"} {
		if ratio == option || ratio == strings.SplitN(option, " ", 2)[0] {
			ratio = option
			validRatio = true
			break
		}
	}
	if !validRatio {
		return nil, fmt.Errorf("视频画幅不受当前工作流支持")
	}
	if input.Megapixels <= 0 || math.IsNaN(input.Megapixels) || math.IsInf(input.Megapixels, 0) {
		return nil, fmt.Errorf("视频分辨率必须大于 0")
	}
	inputs := []NodeInput{
		{NodeID: "263", FieldName: "text", FieldValue: prompt},
		{NodeID: "259", FieldName: "value", FieldValue: input.Duration},
		{NodeID: "252", FieldName: "aspect_ratio", FieldValue: ratio},
		{NodeID: "252", FieldName: "megapixels", FieldValue: strconv.FormatFloat(input.Megapixels, 'f', -1, 64)},
	}
	// Slot order follows the links into MiniMaxH3ReferenceToVideo node 265,
	// rather than the numeric order of loader nodes in the exported graph.
	for _, media := range []struct {
		label string
		field string
		nodes []string
		files []string
	}{
		{"参考图片", "image", []string{"51", "49", "50", "43", "19", "23", "199", "200", "201"}, input.Images},
		{"参考视频", "video", []string{"27", "25", "26"}, input.Videos},
		{"参考音频", "audio", []string{"48", "14", "15"}, input.Audios},
	} {
		if len(media.files) > len(media.nodes) {
			return nil, fmt.Errorf("%s最多支持 %d 个", media.label, len(media.nodes))
		}
		for i, node := range media.nodes {
			value := "None"
			if i < len(media.files) {
				value = strings.TrimSpace(media.files[i])
				if value == "" || value == "None" {
					return nil, fmt.Errorf("%s第 %d 个素材缺少上传文件名", media.label, i+1)
				}
			}
			// Clear unused slots explicitly so the published workflow cannot
			// silently reuse example media left in its saved loader widgets.
			inputs = append(inputs, NodeInput{NodeID: node, FieldName: media.field, FieldValue: value})
		}
	}
	return inputs, nil
}
