package server

import (
	"crypto/subtle"
	"encoding/json"
	"log"
	"net/http"
	"path"
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
		writeOK(w, http.StatusOK, true)
	case "/webhooks/supasend":
		s.handleSupasend(w, r)
	case "/webhooks/file":
		s.handleFile(w, r)
	default:
		writeOK(w, http.StatusNotFound, false)
	}
}

func (s *Server) handleSupasend(w http.ResponseWriter, r *http.Request) {
	s.handleCapture(w, r, decodeSupasendCapture)
}

func (s *Server) handleFile(w http.ResponseWriter, r *http.Request) {
	s.handleCapture(w, r, decodeFileCapture)
}

func (s *Server) handleCapture(
	w http.ResponseWriter,
	r *http.Request,
	decode func(r *http.Request, fallbackCreatedAt time.Time) (captureRequest, error),
) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if !validBearer(r.Header.Get("Authorization"), s.cfg.WebhookToken) {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxPayloadBytes)
	capture, err := decode(r, s.now())
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

	log.Printf(
		"capture saved: source=%s note=%s attachment=%s commit=%s",
		capture.Source,
		result.NotePath,
		result.AttachmentPath,
		result.CommitSHA,
	)
	writeOK(w, http.StatusOK, true)
}

func (s *Server) captureToGitHub(r *http.Request, capture captureRequest) (captureResponse, error) {
	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		result, err := s.captureToGitHubOnce(r, capture)
		if err == nil {
			return result, nil
		}
		if !githubapi.IsRetryable(err) {
			return captureResponse{}, err
		}

		lastErr = err
		time.Sleep(time.Duration(attempt+1) * 200 * time.Millisecond)
	}

	return captureResponse{}, lastErr
}

func (s *Server) captureToGitHubOnce(r *http.Request, capture captureRequest) (captureResponse, error) {
	var files []githubapi.File
	var attachmentPath string
	var attachmentContentType string

	switch {
	case capture.FileURL != "":
		attachment, err := supasend.DownloadAttachment(
			r.Context(),
			s.httpClient,
			capture.FileURL,
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
	case capture.FileName != "":
		attachmentPath = path.Join(
			s.cfg.AttachmentDir,
			capture.CreatedAt.UTC().Format("2006-01-02T15-04-05")+"-"+capture.FileName,
		)
		attachmentContentType = capture.FileContentType
		files = append(files, githubapi.File{Path: attachmentPath, Content: capture.FileContent})
	}

	notePath := note.Path(s.cfg.NoteDir, capture.CreatedAt)
	desiredPaths := make([]string, 0, len(files)+1)
	for _, file := range files {
		desiredPaths = append(desiredPaths, file.Path)
	}
	desiredPaths = append(desiredPaths, notePath)

	uniquePaths, err := s.github.UniquePaths(
		r.Context(),
		s.cfg.GitHubOwner,
		s.cfg.GitHubRepo,
		s.cfg.GitHubBranch,
		desiredPaths,
	)
	if err != nil {
		return captureResponse{}, err
	}

	for i := range files {
		files[i].Path = uniquePaths[i]
		attachmentPath = uniquePaths[i]
	}
	notePath = uniquePaths[len(uniquePaths)-1]

	noteContent := note.Render(note.Capture{
		Source:                capture.Source,
		Text:                  capture.Text,
		CreatedAt:             capture.CreatedAt,
		DueDateUTC:            capture.DueDateUTC,
		FileURL:               capture.FileURL,
		FileName:              capture.FileName,
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
	writeOK(w, status, false)
}

func writeOK(w http.ResponseWriter, status int, ok bool) {
	writeJSON(w, status, okResponse{OK: ok})
}

type captureResponse struct {
	CommitSHA      string `json:"commit_sha"`
	NotePath       string `json:"note_path"`
	AttachmentPath string `json:"attachment_path,omitempty"`
}

type okResponse struct {
	OK bool `json:"ok"`
}

type captureRequest struct {
	Source          string
	Text            string
	FileURL         string
	FileName        string
	FileContent     []byte
	FileContentType string
	DueDateUTC      string
	CreatedAt       time.Time
}
