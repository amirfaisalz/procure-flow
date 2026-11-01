// Package testsupport provides in-memory test doubles for the repository
// ports. It exists so handler, middleware, and application tests stay fast and
// hermetic.
//
// PostgreSQL is the only runtime storage backend: no command under cmd/ may
// import this package. CI enforces that separation.
package testsupport
