package safetransport

// Package safetransport provides an http.Transport that will reject attempts
// to dial internal/private networks.
//
// This code was minimally lightly adapted from Andrew Ayer's excellent "Preventing
// Server Side Request Forgery in Golang" block post:
// https://www.agwa.name/blog/post/preventing_server_side_request_forgery_in_golangs
