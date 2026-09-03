package domain

import "testing"

func TestExitCode(t *testing.T) {
	tests := []struct {
		code ErrorCode
		want int
	}{
		{CodeNotImplemented, 1}, {CodeInvalidInput, 2}, {CodeResyncRequired, 2},
		{CodeAuthRequired, 3}, {CodeNotFound, 4}, {CodeConnectivity, 5},
		{CodeRateLimitedLocal, 6}, {CodeConfirmationMismatch, 7},
		{CodeAmbiguousExternalState, 8}, {CodeInterrupted, 130}, {CodeTerminated, 143},
	}
	for _, test := range tests {
		t.Run(string(test.code), func(t *testing.T) {
			if got := ExitCode(NewError(test.code, "safe")); got != test.want {
				t.Fatalf("ExitCode() = %d, want %d", got, test.want)
			}
		})
	}
	if got := ExitCode(nil); got != 0 {
		t.Fatalf("ExitCode(nil) = %d", got)
	}
}
