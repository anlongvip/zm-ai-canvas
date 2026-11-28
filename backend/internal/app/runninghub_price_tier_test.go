package app

import (
	"testing"

	"yingce/backend/internal/model"
)

func TestRunningHubSaleMatchesActualResolution(t *testing.T) {
	channel := model.ChannelModel{Protocol: model.ChannelInterfaceRunningHubVideo, PriceTiers: []model.ChannelModelPriceTier{
		{ID: "480", Resolution: "480p", PriceConfigured: true, Enabled: true},
		{ID: "720", Resolution: "720p", PriceConfigured: true, Enabled: true},
		{ID: "default", PriceConfigured: true, Enabled: true},
	}}
	intent := ModelRequestIntent{Capability: "video", Options: map[string]any{"vquality": "720p"}}
	s := &Service{}
	for _, id := range []string{"480", "default"} {
		if tier, err := s.channelModelPriceTierForBilling(channel, id, "video", intent); err == nil || tier != nil {
			t.Fatalf("accepted price tier %q for a 720p job", id)
		}
	}
	for _, id := range []string{"", "720"} {
		if tier, err := s.channelModelPriceTierForBilling(channel, id, "video", intent); err != nil || tier == nil || tier.ID != "720" {
			t.Fatalf("expected 720p price tier, got %#v, %v", tier, err)
		}
	}
}
