package mock

import (
	"fmt"
	"time"

	"github.com/paramify/evidence-tui-prototype/internal/runner"
)

type RunStatus = runner.Status

const (
	StatusQueued    = runner.StatusQueued
	StatusRunning   = runner.StatusRunning
	StatusOK        = runner.StatusOK
	StatusPartial   = runner.StatusPartial
	StatusFailed    = runner.StatusFailed
	StatusCancelled = runner.StatusCancelled
)

const StallThreshold = runner.StallThreshold

type Beat struct {
	Delay time.Duration
	Line  string
}

type Script struct {
	Beats      []Beat
	FinalDelay time.Duration
	Final      runner.Status // OK / Partial / Failed / Cancelled
	ExitCode   int
}

func Build(f Fetcher) Script {
	switch f.Behavior {
	case BehaviorQuick:
		return Script{
			Beats: append(beatify(Banner(f.Name), 60*time.Millisecond),
				Beat{350 * time.Millisecond, "  ok: 12 records"},
				Beat{300 * time.Millisecond, "  ok: rotation enabled on all keys"},
			),
			FinalDelay: 600 * time.Millisecond,
			Final:      runner.StatusOK,
		}
	case BehaviorNormal:
		beats := beatify(Banner(f.Name), 80*time.Millisecond)
		for i := 0; i < 6; i++ {
			beats = append(beats, Beat{
				380 * time.Millisecond,
				fmt.Sprintf("  ok: %s shard %d compliant", f.Source, i+1),
			})
		}
		beats = append(beats, beatify(Footer(120), 200*time.Millisecond)...)
		return Script{Beats: beats, FinalDelay: 400 * time.Millisecond, Final: runner.StatusOK}
	case BehaviorStreaming:
		beats := beatify(Banner(f.Name), 60*time.Millisecond)
		for _, ln := range JSONLines(80, f.Source) {
			beats = append(beats, Beat{30 * time.Millisecond, ln})
		}
		beats = append(beats, beatify(Footer(80), 120*time.Millisecond)...)
		return Script{Beats: beats, FinalDelay: 200 * time.Millisecond, Final: runner.StatusOK}
	case BehaviorSlowStart:
		beats := beatify(Banner(f.Name), 80*time.Millisecond)
		beats = append(beats,
			Beat{5500 * time.Millisecond, "  … API rate limited; backing off"},
		)
		for i := 0; i < 8; i++ {
			beats = append(beats, Beat{
				200 * time.Millisecond,
				fmt.Sprintf("  ok: scanned %d/8 endpoints", i+1),
			})
		}
		beats = append(beats, beatify(Footer(8), 200*time.Millisecond)...)
		return Script{Beats: beats, FinalDelay: 300 * time.Millisecond, Final: runner.StatusOK}
	case BehaviorStall:
		beats := beatify(Banner(f.Name), 80*time.Millisecond)
		beats = append(beats,
			Beat{350 * time.Millisecond, "  ok: collecting parameter groups"},
			Beat{350 * time.Millisecond, "  ok: 14 instances enumerated"},
			Beat{6500 * time.Millisecond, "  … resumed: response received from describe-db-parameters"},
			Beat{300 * time.Millisecond, "  ok: rds.force_ssl = 1 on all parameter groups"},
		)
		beats = append(beats, beatify(Footer(14), 150*time.Millisecond)...)
		return Script{Beats: beats, FinalDelay: 250 * time.Millisecond, Final: runner.StatusOK}
	case BehaviorPartial:
		beats := beatify(Banner(f.Name), 80*time.Millisecond)
		beats = append(beats,
			Beat{350 * time.Millisecond, "  ok: enumerated 47 policies"},
			Beat{300 * time.Millisecond, "  WARN: 2 policies grant iam:PassRole on *"},
			Beat{300 * time.Millisecond, "  WARN: 1 inline policy has wildcard action"},
			Beat{300 * time.Millisecond, "  ok: writing inventory snapshot"},
		)
		beats = append(beats, beatify(Footer(47), 150*time.Millisecond)...)
		return Script{Beats: beats, FinalDelay: 250 * time.Millisecond, Final: runner.StatusPartial}
	case BehaviorHardFail:
		beats := beatify(Banner(f.Name), 80*time.Millisecond)
		beats = append(beats,
			Beat{400 * time.Millisecond, "  … listing web ACLs"},
			Beat{600 * time.Millisecond, "  ERROR: AccessDenied: User is not authorized to perform: wafv2:ListWebACLs"},
			Beat{200 * time.Millisecond, "  hint: attach the ParamifyEvidenceReader policy to your role"},
		)
		return Script{Beats: beats, FinalDelay: 200 * time.Millisecond, Final: runner.StatusFailed, ExitCode: 1}
	}
	return Script{
		Beats:      []Beat{{500 * time.Millisecond, "  ok: stub"}},
		FinalDelay: 200 * time.Millisecond,
		Final:      runner.StatusOK,
	}
}

func beatify(lines []string, delay time.Duration) []Beat {
	out := make([]Beat, 0, len(lines))
	for _, ln := range lines {
		out = append(out, Beat{delay, ln})
	}
	return out
}
