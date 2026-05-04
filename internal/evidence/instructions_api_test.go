package evidence

import "testing"

func TestRichTextToParamifyAPIString(t *testing.T) {
	nodes := []RichNode{
		paragraph(spanBold("Script:"), spanText(" "), spanCode("demo.sh")),
		paragraph(spanText("")),
		paragraph(spanBold("Commands: ")),
		unorderedList(listItem(spanCode("aws sts get-caller-identity"))),
	}
	got := RichTextToParamifyAPIString(nodes)
	want := "**Script:** `demo.sh`\n**Commands: **\n• `aws sts get-caller-identity`"
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}
