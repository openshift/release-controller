package main

import (
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestBackoff(t *testing.T) {
	tests := []struct {
		name    string
		step    int
		initial time.Time
		current time.Time
		want    time.Duration
	}{
		{step: 0, initial: time.Unix(0, 0), current: time.Unix(0, 0), want: time.Minute * 0},
		{step: 2, initial: time.Unix(0, 0), current: time.Unix(0, 0), want: time.Minute * 4},
		{step: 4, initial: time.Unix(0, 0), current: time.Unix(0, 0), want: time.Minute * 15},
		{step: 5, initial: time.Unix(0, 0), current: time.Unix(0, 0), want: time.Minute * 15},
		{step: 4, initial: time.Unix(0, 0), current: time.Unix(2000, 0), want: time.Minute * 0},
		{step: 4, initial: time.Unix(0, 0), current: time.Unix(450, 0), want: time.Second * 450},
		{step: 4, initial: time.Unix(450, 0), current: time.Unix(0, 0), want: time.Second * 1350},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := calculateBackoff(tt.step, &metav1.Time{tt.initial}, &metav1.Time{tt.current})
			if got != tt.want {
				t.Errorf("calculateBackoff(%d, %d, %d): want %v, got %v", tt.step, tt.initial.Unix(), tt.current.Unix(), tt.want, got)
			}
		})
	}
}
