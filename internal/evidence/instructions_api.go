package evidence

import "strings"

// RichTextToParamifyAPIString flattens RichNode trees to Paramify POST /evidence instruction text (p and ul only).
func RichTextToParamifyAPIString(nodes []RichNode) string {
	if len(nodes) == 0 {
		return ""
	}
	var b strings.Builder
	for _, item := range nodes {
		switch item.Type {
		case "p":
			var paragraph strings.Builder
			for _, child := range item.Children {
				writeSpan(&paragraph, child)
			}
			if strings.TrimSpace(paragraph.String()) != "" {
				if b.Len() > 0 {
					b.WriteByte('\n')
				}
				b.WriteString(paragraph.String())
			}
		case "ul":
			for _, li := range item.Children {
				if li.Type != "li" {
					continue
				}
				for _, lic := range li.Children {
					if lic.Type != "lic" {
						continue
					}
					var listItemText strings.Builder
					for _, licChild := range lic.Children {
						writeSpan(&listItemText, licChild)
					}
					if strings.TrimSpace(listItemText.String()) != "" {
						if b.Len() > 0 {
							b.WriteByte('\n')
						}
						b.WriteString("• ")
						b.WriteString(listItemText.String())
					}
				}
			}
		default:
			if len(item.Children) > 0 {
				sub := RichTextToParamifyAPIString(item.Children)
				if sub != "" {
					if b.Len() > 0 {
						b.WriteByte('\n')
					}
					b.WriteString(sub)
				}
			}
		}
	}
	return b.String()
}

func writeSpan(b *strings.Builder, child RichNode) {
	if child.Bold {
		b.WriteString("**")
		b.WriteString(child.Text)
		b.WriteString("**")
		return
	}
	if child.Code {
		b.WriteString("`")
		b.WriteString(child.Text)
		b.WriteString("`")
		return
	}
	if child.Text != "" || (!child.Bold && !child.Code) {
		b.WriteString(child.Text)
	}
	for _, c := range child.Children {
		writeSpan(b, c)
	}
}
