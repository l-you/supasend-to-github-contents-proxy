package server

import (
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"log"
	"net/http"
	"path"
	"strings"
	"time"

	"github.com/l-you/supasend-to-github-contents-proxy/internal/config"
	githubapi "github.com/l-you/supasend-to-github-contents-proxy/internal/github"
	"github.com/l-you/supasend-to-github-contents-proxy/internal/note"
	"github.com/l-you/supasend-to-github-contents-proxy/internal/repopath"
	"github.com/l-you/supasend-to-github-contents-proxy/internal/supasend"
)

const maxPayloadBytes = 1024 * 1024
const maxDuplicateIndex = 5

type Server struct {
	cfg        config.Config
	github     *githubapi.Client
	httpClient *http.Client
}

func New(cfg config.Config, githubClient *githubapi.Client, httpClient *http.Client) *Server {
	if httpClient == nil {
		httpClient = http.DefaultClient
	}

	return &Server{
		cfg:        cfg,
		github:     githubClient,
		httpClient: httpClient,
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
		writeError(w, http.StatusNotFound, "not found")
	}
}

func (s *Server) handleSupasend(w http.ResponseWriter, r *http.Request) {
	s.handleCapture(w, r, maxPayloadBytes, decodeSupasendCapture)
}

func (s *Server) handleFile(w http.ResponseWriter, r *http.Request) {
	s.handleCapture(
		w,
		r,
		filePayloadMaxBytes(s.cfg.MaxAttachmentSize),
		func(r *http.Request) (captureRequest, error) {
			return decodeFileCapture(r, s.cfg.MaxAttachmentSize)
		},
	)
}

func (s *Server) handleCapture(
	w http.ResponseWriter,
	r *http.Request,
	maxRequestBytes int64,
	decode func(r *http.Request) (captureRequest, error),
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

	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBytes)
	capture, err := decode(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	result, err := s.captureToGitHub(r, capture)
	if err != nil {
		log.Printf("capture failed: %v", err)
		if githubapi.IsPathUnavailable(err) || githubapi.IsPathExists(err) {
			writeError(w, http.StatusConflict, err.Error())
			return
		}
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
	if capture.FolderName != "" {
		return s.fileCaptureToGitHubOnce(r, capture)
	}

	var files []githubapi.File
	var attachmentPath string
	var attachmentName string
	var attachmentContent []byte

	switch {
	case capture.FileURL != "":
		attachment, err := supasend.DownloadAttachment(
			r.Context(),
			s.httpClient,
			capture.FileURL,
			s.cfg.MaxAttachmentSize,
		)
		if err != nil {
			return captureResponse{}, err
		}

		attachmentName = attachment.FileName
		attachmentContent = attachment.Content
	case capture.AttachmentName != "":
		attachmentName = capture.AttachmentName
		attachmentContent = capture.AttachmentContent
	}

	notePath, noteName, err := s.allocateNotePath(r, capture, attachmentName != "")
	if err != nil {
		return captureResponse{}, err
	}

	if attachmentName != "" {
		extension := path.Ext(attachmentName)
		attachmentPath = path.Join(path.Dir(notePath), noteName+extension)
		files = append(files, githubapi.File{Path: attachmentPath, Content: attachmentContent})
	}
	attachmentBaseName := ""
	if attachmentPath != "" {
		attachmentBaseName = path.Base(attachmentPath)
	}

	noteContent := note.Render(note.Capture{
		Source:         capture.Source,
		Text:           capture.Text,
		CreatedAt:      capture.CreatedAt,
		FileURL:        capture.FileURL,
		NoteName:       noteName,
		AttachmentName: attachmentBaseName,
		AttachmentPath: attachmentPath,
	})
	files = append(files, githubapi.File{Path: notePath, Content: noteContent})

	commit, err := s.github.CommitFiles(
		r.Context(),
		s.cfg.GitHubOwner,
		s.cfg.GitHubRepo,
		s.cfg.GitHubBranch,
		"Add Supasend capture",
		files,
		githubapi.CommitOptions{},
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

func (s *Server) fileCaptureToGitHubOnce(r *http.Request, capture captureRequest) (captureResponse, error) {
	files := make([]githubapi.File, 0, 2)
	var notePath string
	var attachmentPath string

	if capture.AttachmentName != "" {
		attachmentPath = repopath.File(s.cfg.NoteDir, capture.FolderName, capture.AttachmentName)
		files = append(files, githubapi.File{Path: attachmentPath, Content: capture.AttachmentContent})
	}

	if capture.Text != "" {
		notePath = repopath.File(s.cfg.NoteDir, capture.FolderName, capture.NoteFileName)
		attachmentBaseName := ""
		if attachmentPath != "" {
			attachmentBaseName = path.Base(attachmentPath)
		}

		noteContent := note.Render(note.Capture{
			Source:         capture.Source,
			Text:           capture.Text,
			CreatedAt:      capture.CreatedAt,
			NoteName:       capture.NoteFileName,
			AttachmentName: attachmentBaseName,
			AttachmentPath: attachmentPath,
		})
		files = append(files, githubapi.File{Path: notePath, Content: noteContent})
	}

	commit, err := s.github.CommitFiles(
		r.Context(),
		s.cfg.GitHubOwner,
		s.cfg.GitHubRepo,
		s.cfg.GitHubBranch,
		"Add file capture",
		files,
		githubapi.CommitOptions{RejectExisting: true},
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

func (s *Server) allocateNotePath(
	r *http.Request,
	capture captureRequest,
	hasAttachment bool,
) (string, string, error) {
	noteName := capture.NoteName
	if noteName == "" {
		noteName = capture.CreatedAt.UTC().Format("2006-01-02T15-04-05")
	}

	if hasAttachment {
		folderPath, err := s.github.UniqueDirectory(
			r.Context(),
			s.cfg.GitHubOwner,
			s.cfg.GitHubRepo,
			s.cfg.GitHubBranch,
			path.Join(s.cfg.NoteDir, noteName),
			maxDuplicateIndex,
		)
		if err != nil {
			return "", "", err
		}

		folderName := path.Base(folderPath)
		return path.Join(folderPath, folderName+".md"), folderName, nil
	}

	notePath := path.Join(s.cfg.NoteDir, noteName+".md")
	uniquePaths, err := s.github.UniquePaths(
		r.Context(),
		s.cfg.GitHubOwner,
		s.cfg.GitHubRepo,
		s.cfg.GitHubBranch,
		[]string{notePath},
		maxDuplicateIndex,
	)
	if err != nil {
		return "", "", err
	}

	uniqueNotePath := uniquePaths[0]
	uniqueNoteName := strings.TrimSuffix(path.Base(uniqueNotePath), path.Ext(uniqueNotePath))
	return uniqueNotePath, uniqueNoteName, nil
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
	writeJSON(w, status, errorResponse{OK: false, Error: message})
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

type errorResponse struct {
	OK    bool   `json:"ok"`
	Error string `json:"error"`
}

type captureRequest struct {
	Source            string
	Text              string
	FileURL           string
	FolderName        string
	NoteName          string
	NoteFileName      string
	AttachmentName    string
	AttachmentContent []byte
	CreatedAt         time.Time
}

func filePayloadMaxBytes(maxAttachmentBytes int64) int64 {
	if maxAttachmentBytes <= 0 {
		return maxPayloadBytes
	}

	return int64(base64.StdEncoding.EncodedLen(int(maxAttachmentBytes))) + maxPayloadBytes
}
