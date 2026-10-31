package runninghub

import (
	"fmt"
	"strconv"
	"strings"
)

type VideoInputs struct {
	Prompt      string
	Duration    int
	AspectRatio string
	Megapixels  float64
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
	for _, option := range []string{"1:1 (Square)", "2:3 (Portrait Photo)", "3:2 (Photo)", "3:4 (Portrait Standard)", "4:3 (Standard)", "9:16 (Portrait Widescreen)", "16:9 (Widescreen)", "21:9 (Ultrawide)"} {
		if ratio == strings.SplitN(option, " ", 2)[0] {
			ratio = option
			break
		}
	}
	if input.Megapixels <= 0 {
		return nil, fmt.Errorf("视频分辨率必须大于 0")
	}
	return []NodeInput{
		{NodeID: "263", FieldName: "text", FieldValue: prompt},
		{NodeID: "259", FieldName: "value", FieldValue: input.Duration},
		{NodeID: "252", FieldName: "aspect_ratio", FieldValue: ratio},
		{NodeID: "252", FieldName: "megapixels", FieldValue: strconv.FormatFloat(input.Megapixels, 'f', -1, 64)},
	}, nil
}
