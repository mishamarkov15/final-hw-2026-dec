package cache

import (
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMain(m *testing.M) {
	log.SetOutput(io.Discard)
	os.Exit(m.Run())
}

// newTestCache поднимает Redis в памяти (miniredis) и подключает к нему Cache.
func newTestCache(t *testing.T) (*Cache, *miniredis.Miniredis) {
	t.Helper()
	mr := miniredis.RunT(t)
	c, err := New(context.Background(), Config{Addr: mr.Addr()})
	require.NoError(t, err)
	t.Cleanup(func() { c.Close() })
	return c, mr
}

func TestConfigFromEnv(t *testing.T) {
	tests := []struct {
		name    string
		env     map[string]string
		want    Config
		wantErr bool
	}{
		{name: "defaults", want: Config{Addr: "localhost:6379"}},
		{
			name: "custom",
			env:  map[string]string{"REDIS_ADDR": "redis:6380", "REDIS_DB": "2", "REDIS_PASSWORD": "secret"},
			want: Config{Addr: "redis:6380", DB: 2, Password: "secret"},
		},
		{name: "db_not_number", env: map[string]string{"REDIS_DB": "first"}, wantErr: true},
		{name: "db_negative", env: map[string]string{"REDIS_DB": "-1"}, wantErr: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			for _, key := range []string{"REDIS_ADDR", "REDIS_DB", "REDIS_PASSWORD"} {
				t.Setenv(key, tc.env[key])
			}

			cfg, err := ConfigFromEnv()

			if tc.wantErr {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.want, cfg)
		})
	}
}

func TestNew(t *testing.T) {
	ctx := context.Background()

	t.Run("unreachable", func(t *testing.T) {
		mr := miniredis.RunT(t)
		addr := mr.Addr()
		mr.Close()

		_, err := New(ctx, Config{Addr: addr})

		assert.Error(t, err)
	})

	t.Run("password", func(t *testing.T) {
		mr := miniredis.RunT(t)
		mr.RequireAuth("secret")

		_, err := New(ctx, Config{Addr: mr.Addr(), Password: "wrong"})
		assert.Error(t, err)

		c, err := New(ctx, Config{Addr: mr.Addr(), Password: "secret"})
		require.NoError(t, err)
		assert.NoError(t, c.Close())
	})

	t.Run("select_db", func(t *testing.T) {
		mr := miniredis.RunT(t)
		c, err := New(ctx, Config{Addr: mr.Addr(), DB: 3})
		require.NoError(t, err)
		t.Cleanup(func() { c.Close() })

		require.NoError(t, c.SetJSON(ctx, "key", 1, time.Minute))

		mr.Select(3)
		assert.True(t, mr.Exists("key"))
	})
}

func TestJSON(t *testing.T) {
	type report struct {
		Total float64  `json:"total"`
		Items []string `json:"items"`
	}
	ctx := context.Background()

	t.Run("miss", func(t *testing.T) {
		c, _ := newTestCache(t)

		var got report
		hit, err := c.GetJSON(ctx, "report:summary:none", &got)

		require.NoError(t, err)
		assert.False(t, hit)
	})

	t.Run("set_then_get", func(t *testing.T) {
		c, mr := newTestCache(t)
		want := report{Total: 450.5, Items: []string{"еда"}}

		require.NoError(t, c.SetJSON(ctx, "report:summary:a", want, 30*time.Second))
		var got report
		hit, err := c.GetJSON(ctx, "report:summary:a", &got)

		require.NoError(t, err)
		assert.True(t, hit)
		assert.Equal(t, want, got)
		assert.Equal(t, 30*time.Second, mr.TTL("report:summary:a"))
	})

	t.Run("expires_after_ttl", func(t *testing.T) {
		c, mr := newTestCache(t)
		require.NoError(t, c.SetJSON(ctx, "key", report{Total: 1}, 30*time.Second))

		mr.FastForward(31 * time.Second)
		var got report
		hit, err := c.GetJSON(ctx, "key", &got)

		require.NoError(t, err)
		assert.False(t, hit)
	})

	t.Run("broken_value", func(t *testing.T) {
		c, mr := newTestCache(t)
		require.NoError(t, mr.Set("key", "not json"))

		var got report
		hit, err := c.GetJSON(ctx, "key", &got)

		assert.Error(t, err)
		assert.False(t, hit)
	})

	t.Run("unserializable_value", func(t *testing.T) {
		c, _ := newTestCache(t)

		err := c.SetJSON(ctx, "key", func() {}, time.Minute)

		assert.Error(t, err)
	})

	t.Run("redis_down", func(t *testing.T) {
		c, mr := newTestCache(t)
		mr.Close()

		var got report
		_, getErr := c.GetJSON(ctx, "key", &got)
		setErr := c.SetJSON(ctx, "key", report{}, time.Minute)

		assert.Error(t, getErr)
		assert.Error(t, setErr)
	})
}

func TestDelete(t *testing.T) {
	ctx := context.Background()

	t.Run("by_key", func(t *testing.T) {
		c, mr := newTestCache(t)
		require.NoError(t, mr.Set("budgets:all", "[]"))

		require.NoError(t, c.Delete(ctx, "budgets:all", "missing"))

		assert.False(t, mr.Exists("budgets:all"))
	})

	t.Run("by_prefix", func(t *testing.T) {
		c, mr := newTestCache(t)
		for _, key := range []string{"report:summary:a", "report:summary:b", "budgets:all"} {
			require.NoError(t, mr.Set(key, "{}"))
		}

		require.NoError(t, c.DeleteByPrefix(ctx, "report:summary:"))

		assert.Equal(t, []string{"budgets:all"}, mr.Keys())
	})

	t.Run("by_prefix_many_keys", func(t *testing.T) {
		c, mr := newTestCache(t)
		for i := 0; i < 250; i++ {
			require.NoError(t, mr.Set(fmt.Sprintf("report:summary:%d", i), "{}"))
		}

		require.NoError(t, c.DeleteByPrefix(ctx, "report:summary:"))

		assert.Empty(t, mr.Keys())
	})

	t.Run("by_prefix_no_keys", func(t *testing.T) {
		c, _ := newTestCache(t)

		assert.NoError(t, c.DeleteByPrefix(ctx, "report:summary:"))
	})

	t.Run("redis_down", func(t *testing.T) {
		c, mr := newTestCache(t)
		mr.Close()

		assert.Error(t, c.DeleteByPrefix(ctx, "report:summary:"))
	})
}
