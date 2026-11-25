package metaldogs

import (
	"context"
	"fmt"
	"net/url"
	"strings"

	"github.com/akarso/shopanda/pkg/extapi"
	"github.com/akarso/shopanda/pkg/pluginsdk"
)

const orderLineDisplayFieldCode = "metaldogs.thumbnail"

const (
	orderLineDisplaySkipEvent = "metaldogs.order_line_display.skipped"
	orderLineDisplayFailEvent = "metaldogs.order_line_display.failed"
	orderLineDisplayHTTPEvent = "metaldogs.order_line_display.public_base_url"
)

type thumbnailCopy struct {
	label string
	alt   string
}

// Primary BCP 47 subtag. Unknown and empty tags use en.
var thumbnailCopyByLang = map[string]thumbnailCopy{
	"en": {label: "Your design", alt: "Preview of your custom design"},
	"pl": {label: "Twój projekt", alt: "Podgląd Twojego projektu"},
	"de": {label: "Dein Design", alt: "Vorschau deines individuellen Designs"},
	"fr": {label: "Votre création", alt: "Aperçu de votre création personnalisée"},
	"es": {label: "Tu diseño", alt: "Vista previa de tu diseño personalizado"},
}

func registerOrderLineDisplay(hooks pluginsdk.HookAPI, p *Plugin, log pluginsdk.Logger) error {
	if p == nil || !p.orderSnapshotEnabled || len(p.thumbnailKey) < 32 || p.publicBaseURL == "" {
		return nil
	}
	// Shopanda drops Kind=image unless the URL is public https.
	if !publicBaseIsHTTPS(p.publicBaseURL) {
		if log != nil {
			log.Warn(orderLineDisplayHTTPEvent, map[string]interface{}{
				"reason": "not_https",
			})
		}
		return nil
	}
	if hooks == nil {
		return fmt.Errorf("metaldogs plugin: order line display: hooks not configured")
	}
	return hooks.Register(extapi.HookOrderLineDisplay, 100, p.onOrderLineDisplay)
}

func publicBaseIsHTTPS(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil || u == nil {
		return false
	}
	return u.Scheme == "https" && u.Host != ""
}

func (p *Plugin) onOrderLineDisplay(hctx *extapi.HookContext) error {
	if hctx == nil || p == nil {
		return nil
	}
	orderID := hctx.OrderLineDisplayOrderID()
	variantID := hctx.OrderLineDisplayVariantID()
	link, err := p.OrderThumbnailURL(context.Background(), orderID, variantID)
	if err != nil || link == "" {
		p.logOrderLineDisplay(orderID, variantID, err != nil)
		return nil
	}
	label, alt := thumbnailLabel(hctx.OrderLineDisplayLanguage())
	hctx.AppendOrderLineDisplayField(extapi.OrderLineDisplayField{
		Code:  orderLineDisplayFieldCode,
		Label: label,
		Kind:  extapi.DisplayKindImage,
		Value: link,
		Alt:   alt,
	})
	return nil
}

func (p *Plugin) logOrderLineDisplay(orderID, variantID string, failed bool) {
	if p == nil || p.host == nil || p.host.Logger() == nil {
		return
	}
	fields := map[string]interface{}{
		"order_id":   orderID,
		"variant_id": variantID,
	}
	if failed {
		p.host.Logger().Warn(orderLineDisplayFailEvent, fields)
		return
	}
	p.host.Logger().Info(orderLineDisplaySkipEvent, fields)
}

func thumbnailLabel(tag string) (label, alt string) {
	primary := thumbnailPrimaryLang(tag)
	copy, ok := thumbnailCopyByLang[primary]
	if !ok {
		copy = thumbnailCopyByLang["en"]
	}
	return copy.label, copy.alt
}

func thumbnailPrimaryLang(tag string) string {
	tag = strings.TrimSpace(tag)
	if tag == "" {
		return "en"
	}
	if i := strings.IndexByte(tag, '-'); i >= 0 {
		tag = tag[:i]
	}
	tag = strings.ToLower(tag)
	if _, ok := thumbnailCopyByLang[tag]; !ok {
		return "en"
	}
	return tag
}
