package handler

import "net/http"

// Stub handler implementations. Each of these is filled out by the
// matching M3 sub-task and replaced as we go (M3.4 = list, M3.5 =
// detail, M3.6 = tags, M3.7 = search, M3.8 = sitemap). The skeleton
// keeps them as 501 stubs so Routes() can be exercised by tests today.

func (s *Server) questionsList(w http.ResponseWriter, r *http.Request) {
	s.listQuestions(w, r)
}

func (s *Server) questionDetail(w http.ResponseWriter, r *http.Request) {
	s.detail(w, r)
}

func (s *Server) tagsIndex(w http.ResponseWriter, r *http.Request) {
	s.listTags(w, r)
}

func (s *Server) tagDetail(w http.ResponseWriter, r *http.Request) {
	s.tagQuestions(w, r)
}

func (s *Server) search(w http.ResponseWriter, r *http.Request) {
	s.searchQuestions(w, r)
}

func (s *Server) sitemapIndex(w http.ResponseWriter, r *http.Request) {
	s.sitemapIndexImpl(w, r)
}

func (s *Server) sitemapPart(w http.ResponseWriter, r *http.Request) {
	s.sitemapPartImpl(w, r)
}
