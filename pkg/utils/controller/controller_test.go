package controller

import (
	"errors"
	"testing"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

func TestNewUnexpectedBehaviorError(t *testing.T) {
	tests := []struct {
		name    string
		err     error
		wantErr error
	}{
		{
			name: "nil error",
			err:  nil,
		},
		{
			name:    "unexpectedBehaviorError",
			err:     errors.New("unexpected"),
			wantErr: ErrUnexpectedBehavior,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := NewUnexpectedBehaviorError(tc.err)
			if tc.err == nil && got != nil {
				t.Fatalf("NewUnexpectedBehaviorError(nil) = %v, want nil", got)
			}
			if tc.err != nil && !errors.Is(got, tc.wantErr) {
				t.Fatalf("NewUnexpectedBehaviorError() = %v, want %v", got, tc.wantErr)
			}
		})
	}
}

func TestNewExpectedBehaviorError(t *testing.T) {
	tests := []struct {
		name    string
		err     error
		wantErr error
	}{
		{
			name: "nil error",
			err:  nil,
		},
		{
			name:    "expectedBehaviorError",
			err:     errors.New("expected"),
			wantErr: ErrExpectedBehavior,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := NewExpectedBehaviorError(tc.err)
			if tc.err == nil && got != nil {
				t.Fatalf("NewExpectedBehaviorError(nil) = %v, want nil", got)
			}
			if tc.err != nil && !errors.Is(got, tc.wantErr) {
				t.Fatalf("NewExpectedBehaviorError() = %v, want %v", got, tc.wantErr)
			}
		})
	}
}

func TestNewAPIServerError(t *testing.T) {
	tests := []struct {
		name      string
		fromCache bool
		err       error
		wantErr   error
	}{
		{
			name: "nil error",
			err:  nil,
		},
		{
			name:      "reading from cache: apiServerError",
			fromCache: true,
			err:       apierrors.NewNotFound(schema.GroupResource{}, "invalid"),
			wantErr:   ErrAPIServerError,
		},
		{
			name:      "reading from cache: unexpectedBehaviorError",
			fromCache: true,
			err:       apierrors.NewConflict(schema.GroupResource{}, "conflict", nil),
			wantErr:   ErrUnexpectedBehavior,
		},
		{
			name:      "reading from API server: apiServerError",
			fromCache: false,
			err:       apierrors.NewNotFound(schema.GroupResource{}, "invalid"),
			wantErr:   ErrAPIServerError,
		},
		{
			name:      "reading from API server: apiServerError",
			fromCache: false,
			err:       apierrors.NewConflict(schema.GroupResource{}, "conflict", nil),
			wantErr:   ErrAPIServerError,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := NewAPIServerError(tc.fromCache, tc.err)
			if tc.err == nil && got != nil {
				t.Fatalf("NewAPIServerError(nil) = %v, want nil", got)
			}
			if tc.err != nil && !errors.Is(got, tc.wantErr) {
				t.Fatalf("NewAPIServerError() = %v, want %v", got, tc.wantErr)
			}
		})
	}
}

func TestNewUserError(t *testing.T) {
	tests := []struct {
		name    string
		err     error
		wantErr error
	}{
		{
			name: "nil error",
			err:  nil,
		},
		{
			name:    "userError",
			err:     errors.New("user error"),
			wantErr: ErrUserError,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := NewUserError(tc.err)
			if tc.err == nil && got != nil {
				t.Fatalf("NewUserError(nil) = %v, want nil", got)
			}
			if tc.err != nil && !errors.Is(got, tc.wantErr) {
				t.Fatalf("NewUserError() = %v, want %v", got, tc.wantErr)
			}
		})
	}
}

func TestNewUpdateIgnoreConflictError(t *testing.T) {
	tests := []struct {
		name    string
		err     error
		wantErr error
	}{
		{
			name: "nil error leads to nil error",
			err:  nil,
		},
		{
			name:    "conflict error is expected",
			err:     apierrors.NewConflict(schema.GroupResource{}, "conflict", nil),
			wantErr: ErrExpectedBehavior,
		},
		{
			name:    "not found error is not expected",
			err:     apierrors.NewNotFound(schema.GroupResource{}, "bad"),
			wantErr: ErrAPIServerError,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotError := NewUpdateIgnoreConflictError(tt.err)
			if tt.err == nil && gotError != nil {
				t.Errorf("NewUpdateIgnoreConflictError() error = %v, nil", gotError)
			}
			if tt.err != nil && !errors.Is(gotError, tt.wantErr) {
				t.Fatalf("NewUpdateIgnoreConflictError() = %v, want %v", gotError, tt.wantErr)
			}
		})
	}
}

func TestNewCreateIgnoreAlreadyExistError(t *testing.T) {
	tests := []struct {
		name    string
		err     error
		wantErr error
	}{
		{
			name: "nil error leads to nil error",
			err:  nil,
		},
		{
			name:    "already exist error is expected",
			err:     apierrors.NewAlreadyExists(schema.GroupResource{}, "conflict"),
			wantErr: ErrExpectedBehavior,
		},
		{
			name:    "NewNotFound error is not expected",
			err:     apierrors.NewNotFound(schema.GroupResource{}, "bad"),
			wantErr: ErrAPIServerError,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotError := NewCreateIgnoreAlreadyExistError(tt.err)
			if tt.err == nil && gotError != nil {
				t.Errorf("NewCreateIgnoreAlreadyExistError() error = %v, nil", gotError)
			}
			if tt.err != nil && !errors.Is(gotError, tt.wantErr) {
				t.Fatalf("NewCreateIgnoreAlreadyExistError() = %v, want %v", gotError, tt.wantErr)
			}
		})
	}
}
