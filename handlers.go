package main

import (
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"strings"
)

type Server struct {
	cfg    *Config
	ipsets map[string]IPSet
}

func NewServer(cfg *Config, ipsets map[string]IPSet) *Server {
	return &Server{cfg: cfg, ipsets: ipsets}
}

func (s *Server) Routes() *http.ServeMux {
	mux := http.NewServeMux()
	mux.Handle("POST /{type}/add", RequireBearer(s.cfg.AccessKey, http.HandlerFunc(s.handleAdd)))
	mux.Handle("POST /{type}/del", RequireBearer(s.cfg.AccessKey, http.HandlerFunc(s.handleDel)))
	mux.Handle("GET /{type}/status", RequireBearer(s.cfg.AccessKey, http.HandlerFunc(s.handleStatus)))
	return mux
}

type deviceStatus struct {
	IP      string `json:"ip"`
	Present bool   `json:"present"`
}

type statusResponse struct {
	Type       string         `json:"type"`
	AllPresent bool           `json:"all_present"`
	Devices    []deviceStatus `json:"devices"`
	Members    []string       `json:"members"`
}

type toggleResponse struct {
	Type    string   `json:"type"`
	Members []string `json:"members"`
}

// resolve parses {type} and ?ip=<comma-separated>, validates each IP, writes
// any error response, and returns the matched ipset + parsed IPs. ok=false
// means an error response was already sent.
func (s *Server) resolve(w http.ResponseWriter, r *http.Request) (typ string, ipset IPSet, ips []string, ok bool) {
	typ = r.PathValue("type")
	ipset, found := s.ipsets[typ]
	if !found {
		http.Error(w, "unknown type", http.StatusNotFound)
		return "", nil, nil, false
	}
	raw := r.URL.Query().Get("ip")
	if raw == "" {
		http.Error(w, "ip query parameter required", http.StatusBadRequest)
		return "", nil, nil, false
	}
	for _, part := range strings.Split(raw, ",") {
		ip := strings.TrimSpace(part)
		if ip == "" {
			http.Error(w, "ip query parameter contains empty entry", http.StatusBadRequest)
			return "", nil, nil, false
		}
		if net.ParseIP(ip) == nil {
			http.Error(w, "invalid ip: "+ip, http.StatusBadRequest)
			return "", nil, nil, false
		}
		ips = append(ips, ip)
	}
	return typ, ipset, ips, true
}

func (s *Server) handleAdd(w http.ResponseWriter, r *http.Request) {
	typ, ipset, ips, ok := s.resolve(w, r)
	if !ok {
		return
	}
	var errs []string
	for _, ip := range ips {
		if err := ipset.Add(ip); err != nil {
			errs = append(errs, ip+": "+err.Error())
		}
	}
	members, err := ipset.Members()
	if err != nil {
		errs = append(errs, "list: "+err.Error())
	}
	if len(errs) > 0 {
		http.Error(w, "ipset errors: "+strings.Join(errs, "; "), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, toggleResponse{Type: typ, Members: members})
}

func (s *Server) handleDel(w http.ResponseWriter, r *http.Request) {
	typ, ipset, ips, ok := s.resolve(w, r)
	if !ok {
		return
	}
	var errs []string
	for _, ip := range ips {
		if err := ipset.Del(ip); err != nil {
			errs = append(errs, ip+": "+err.Error())
		}
	}
	members, err := ipset.Members()
	if err != nil {
		errs = append(errs, "list: "+err.Error())
	}
	if len(errs) > 0 {
		http.Error(w, "ipset errors: "+strings.Join(errs, "; "), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, toggleResponse{Type: typ, Members: members})
}

func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	typ, ipset, ips, ok := s.resolve(w, r)
	if !ok {
		return
	}
	members, err := ipset.Members()
	if err != nil {
		http.Error(w, "ipset list: "+err.Error(), http.StatusInternalServerError)
		return
	}
	memberSet := make(map[string]struct{}, len(members))
	for _, m := range members {
		memberSet[m] = struct{}{}
	}

	devices := make([]deviceStatus, 0, len(ips))
	allPresent := len(ips) > 0
	for _, ip := range ips {
		_, present := memberSet[ip]
		if !present {
			allPresent = false
		}
		devices = append(devices, deviceStatus{IP: ip, Present: present})
	}

	writeJSON(w, http.StatusOK, statusResponse{
		Type:       typ,
		AllPresent: allPresent,
		Devices:    devices,
		Members:    members,
	})
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(body); err != nil && !errors.Is(err, http.ErrHandlerTimeout) {
		_ = err
	}
}
