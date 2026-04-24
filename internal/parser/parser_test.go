package parser

import (
	"reflect"
	"strings"
	"testing"
)

func TestRegistryParse(t *testing.T) {
	registry := NewRegistry()

	t.Run("dispatches nginx parser", func(t *testing.T) {
		result, err := registry.Parse("nginx", `
server {
    listen 80;
    location / {
        proxy_pass http://localhost:3000;
    }
}`)
		if err != nil {
			t.Fatalf("Parse returned error: %v", err)
		}
		if len(result.Forwards) != 1 {
			t.Fatalf("Parse returned %d forwards, want 1", len(result.Forwards))
		}
		if result.Forwards[0].Target.Host != "localhost" || result.Forwards[0].Target.Port != 3000 {
			t.Fatalf("unexpected target: %#v", result.Forwards[0].Target)
		}
	})

	t.Run("rejects unknown kind", func(t *testing.T) {
		_, err := registry.Parse("haproxy", "frontend x")
		if err == nil {
			t.Fatal("expected error")
		}
		if !strings.Contains(err.Error(), `unsupported parser kind "haproxy"`) {
			t.Fatalf("unexpected error: %v", err)
		}
	})
}

func TestNginxParserParse(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    []ForwardAction
		wantErr bool
	}{
		{
			name: "parses direct proxy_pass",
			content: `
server {
    listen 80;
    server_name app.example.com www.example.com;
    location / {
        proxy_pass http://127.0.0.1:3000;
    }
}`,
			want: []ForwardAction{{
				Listener: Listener{Port: 80},
				Target: ForwardTarget{
					Raw:  "http://127.0.0.1:3000",
					Kind: TargetKindAddress,
					Host: "127.0.0.1",
					Port: 3000,
				},
				Hostnames: []string{"app.example.com", "www.example.com"},
			}},
		},
		{
			name: "expands upstream groups and multiple members",
			content: `
upstream api_backend {
    server 10.0.0.5:8080;
    server 10.0.0.6:8080;
}

server {
    listen 443 ssl;
    location / {
        proxy_pass http://api_backend;
    }
}`,
			want: []ForwardAction{
				{
					Listener: Listener{Port: 443},
					Target: ForwardTarget{
						Raw:  "10.0.0.5:8080",
						Kind: TargetKindAddress,
						Host: "10.0.0.5",
						Port: 8080,
					},
				},
				{
					Listener: Listener{Port: 443},
					Target: ForwardTarget{
						Raw:  "10.0.0.6:8080",
						Kind: TargetKindAddress,
						Host: "10.0.0.6",
						Port: 8080,
					},
				},
			},
		},
		{
			name: "parses grpc and fastcgi unix socket targets",
			content: `
server {
    listen 9000;
    location /rpc {
        grpc_pass grpc://127.0.0.1:9100;
    }
    location ~ \.php$ {
        fastcgi_pass unix:/run/php/php8.2-fpm.sock;
    }
}`,
			want: []ForwardAction{
				{
					Listener: Listener{Port: 9000},
					Target: ForwardTarget{
						Raw:  "grpc://127.0.0.1:9100",
						Kind: TargetKindAddress,
						Host: "127.0.0.1",
						Port: 9100,
					},
				},
				{
					Listener: Listener{Port: 9000},
					Target: ForwardTarget{
						Raw:    "unix:/run/php/php8.2-fpm.sock",
						Kind:   TargetKindUnix,
						Socket: "/run/php/php8.2-fpm.sock",
					},
				},
			},
		},
		{
			name: "errors when no forward directives exist",
			content: `
events {}
http {}`,
			wantErr: true,
		},
	}

	parser := NginxParser{}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parser.Parse(tt.content)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("Parse returned error: %v", err)
			}
			if len(got.Forwards) != len(tt.want) {
				t.Fatalf("Parse returned %d forwards, want %d: %#v", len(got.Forwards), len(tt.want), got.Forwards)
			}
			for i := range tt.want {
				if !reflect.DeepEqual(got.Forwards[i], tt.want[i]) {
					t.Fatalf("forward %d = %#v, want %#v", i, got.Forwards[i], tt.want[i])
				}
			}
		})
	}
}

func TestNginxParserParseBundleWithIncludes(t *testing.T) {
	t.Parallel()

	parser := NginxParser{}
	result, err := parser.ParseBundle("/etc/nginx/nginx.conf", map[string]string{
		"/etc/nginx/nginx.conf": `
include /etc/nginx/conf.d/*.conf;

server {
    listen 80;
    location / {
        proxy_pass http://localhost:3000;
    }
}`,
		"/etc/nginx/conf.d/upstreams.conf": `
upstream dashboard_backend {
    server 10.0.0.5:8080;
}`,
		"/etc/nginx/conf.d/dashboard.conf": `
server {
    listen 8080;
    include snippets/dashboard-routes.conf;
}`,
		"/etc/nginx/conf.d/snippets/dashboard-routes.conf": `
location / {
    proxy_pass http://dashboard_backend;
}`,
	})
	if err != nil {
		t.Fatalf("ParseBundle returned error: %v", err)
	}
	if len(result.Forwards) != 2 {
		t.Fatalf("ParseBundle returned %d forwards, want 2: %#v", len(result.Forwards), result.Forwards)
	}
	if result.Forwards[1].Listener.Port != 8080 || result.Forwards[1].Target.Host != "10.0.0.5" {
		t.Fatalf("unexpected included forward: %#v", result.Forwards[1])
	}
}

func TestCaddyParserParse(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    []ForwardAction
		wantErr bool
	}{
		{
			name: "parses caddyfile reverse_proxy",
			content: `
example.com {
    reverse_proxy localhost:3000
}`,
			want: []ForwardAction{{
				Listener: Listener{Port: 443},
				Target: ForwardTarget{
					Raw:  "localhost:3000",
					Kind: TargetKindAddress,
					Host: "localhost",
					Port: 3000,
				},
				Hostnames: []string{"example.com"},
			}},
		},
		{
			name: "parses explicit listen port and php_fastcgi",
			content: `
:8080 {
    php_fastcgi unix//run/php/php-fpm.sock
}`,
			want: []ForwardAction{{
				Listener: Listener{Port: 8080},
				Target: ForwardTarget{
					Raw:    "unix//run/php/php-fpm.sock",
					Kind:   TargetKindUnix,
					Socket: "/run/php/php-fpm.sock",
				},
			}},
		},
		{
			name: "parses caddy json reverse proxy",
			content: `{
  "apps": {
    "http": {
      "servers": {
        "srv0": {
          "listen": [":443"],
          "routes": [{
            "match": [{"host": ["api.example.com"]}],
            "handle": [{
              "handler": "reverse_proxy",
              "upstreams": [{"dial": "127.0.0.1:8080"}]
            }]
          }]
        }
      }
    }
  }
}`,
			want: []ForwardAction{{
				Listener: Listener{Port: 443},
				Target: ForwardTarget{
					Raw:  "127.0.0.1:8080",
					Kind: TargetKindAddress,
					Host: "127.0.0.1",
					Port: 8080,
				},
				Hostnames: []string{"api.example.com"},
			}},
		},
		{
			name: "parses caddy dynamic upstream as unresolved forward",
			content: `{
  "apps": {
    "http": {
      "servers": {
        "srv0": {
          "listen": [":443"],
          "routes": [{
            "handle": [{
              "handler": "reverse_proxy",
              "dynamic": {"srv": {"name": "backend.internal"}}
            }]
          }]
        }
      }
    }
  }
}`,
			want: []ForwardAction{{
				Listener: Listener{Port: 443},
				Target: ForwardTarget{
					Raw:  "dynamic:srv",
					Kind: TargetKindDynamic,
				},
			}},
		},
		{
			name:    "errors on empty config",
			content: "   ",
			wantErr: true,
		},
	}

	parser := CaddyParser{}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parser.Parse(tt.content)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("Parse returned error: %v", err)
			}
			if len(got.Forwards) != len(tt.want) {
				t.Fatalf("Parse returned %d forwards, want %d: %#v", len(got.Forwards), len(tt.want), got.Forwards)
			}
			for i := range tt.want {
				if !reflect.DeepEqual(got.Forwards[i], tt.want[i]) {
					t.Fatalf("forward %d = %#v, want %#v", i, got.Forwards[i], tt.want[i])
				}
			}
		})
	}
}
