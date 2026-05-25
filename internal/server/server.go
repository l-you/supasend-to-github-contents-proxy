package server

import (
	"crypto/subtle"
	"encoding/json"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/l-you/supasend-to-github-contents-proxy/internal/config"
	githubapi "github.com/l-you/supasend-to-github-contents-proxy/internal/github"
	"github.com/l-you/supasend-to-github-contents-proxy/internal/note"
	"github.com/l-you/supasend-to-github-contents-proxy/internal/supasend"
)

const maxPayloadBytes = 1024 * 1024

type Server struct {
	cfg        config.Config
	github     *githubapi.Client
	httpClient *http.Client
	now        func() time.Time
}

func New(cfg config.Config, githubClient *githubapi.Client, httpClient *http.Client) *Server {
	if httpClient == nil {
		httpClient = http.DefaultClient
	}

	return &Server{
		cfg:        cfg,
		github:     githubClient,
		httpClient: httpClient,
		now:        func() time.Time { return time.Now().UTC() },
	}
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch r.URL.Path {
	case "/healthz":
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	case "/webhooks/supasend":
		s.handleSupasend(w, r)
	default:
		http.NotFound(w, r)
	}
}

func (s *Server) handleSupasend(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if !validBearer(r.Header.Get("Authorization"), s.cfg.WebhookToken) {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	body := http.MaxBytesReader(w, r.Body, maxPayloadBytes)
	capture, err := supasend.DecodePayload(body, s.now())
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	result, err := s.captureToGitHub(r, capture)
	if err != nil {
		log.Printf("capture failed: %v", err)
		writeError(w, http.StatusBadGateway, "failed to write capture")
		return
	}

	status := http.StatusCreated
	if !result.Created {
		status = http.StatusOK
	}
	writeJSON(w, status, result)
}

func (s *Server) captureToGitHub(r *http.Request, capture supasend.Capture) (captureResponse, error) {
	var files []githubapi.File
	var attachmentPath string
	var attachmentContentType string

	if capture.SharedURL != "" {
		attachment, err := supasend.DownloadAttachment(
			r.Context(),
			s.httpClient,
			capture.SharedURL,
			s.cfg.AttachmentDir,
			capture.CreatedAt,
			s.cfg.MaxAttachmentSize,
		)
		if err != nil {
			return captureResponse{}, err
		}

		attachmentPath = attachment.Path
		attachmentContentType = attachment.ContentType
		files = append(files, githubapi.File{Path: attachment.Path, Content: attachment.Content})
	}

	notePath := note.Path(s.cfg.NoteDir, capture.CreatedAt)
	noteContent := note.Render(note.Capture{
		Text:                  capture.Text,
		CreatedAt:             capture.CreatedAt,
		DueDateUTC:            capture.DueDateUTC,
		SharedURL:             capture.SharedURL,
		AttachmentPath:        attachmentPath,
		AttachmentContentType: attachmentContentType,
	})
	files = append(files, githubapi.File{Path: notePath, Content: noteContent})

	commit, err := s.github.CommitFiles(
		r.Context(),
		s.cfg.GitHubOwner,
		s.cfg.GitHubRepo,
		s.cfg.GitHubBranch,
		"Add Supasend capture",
		files,
	)
	if err != nil {
		return captureResponse{}, err
	}

	return captureResponse{
		Created:        commit.Created,
		CommitSHA:      commit.SHA,
		NotePath:       notePath,
		AttachmentPath: attachmentPath,
	}, nil
}

func validBearer(header string, token string) bool {
	const prefix = "Bearer "
	if !strings.HasPrefix(header, prefix) {
		return false
	}

	got := strings.TrimSpace(strings.TrimPrefix(header, prefix))
	return subtle.ConstantTimeCompare([]byte(got), []byte(token)) == 1
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(value); err != nil {
		log.Printf("write response: %v", err)
	}
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, errorResponse{Error: message})
}

type captureResponse struct {
	Created        bool   `json:"created"`
	CommitSHA      string `json:"commit_sha"`
	NotePath       string `json:"note_path"`
	AttachmentPath string `json:"attachment_path,omitempty"`
}

type errorResponse struct {
	Error string `json:"error"`
}
