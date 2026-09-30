// SPDX-License-Identifier: Apache-2.0

package ziti

import (
	"context"
	"crypto/sha1"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"
)

// Fake supports filters of the form `field="v"` and `field in ["a","b"]`, joined by " and ".
// Fields are top-level keys or tags.<key>.
type Fake struct {
	VersionString string
	mu            sync.Mutex
	next          int
	Objects       map[Kind]map[string]Entity
	Calls         []string
}

var _ Client = (*Fake)(nil)

func NewFake() *Fake {
	return &Fake{Objects: map[Kind]map[string]Entity{}}
}

func (f *Fake) Put(kind Kind, e Entity) string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.put(kind, "", clone(e))
}

func (f *Fake) put(kind Kind, id string, e Entity) string {
	if id == "" {
		if id = e.ID(); id == "" {
			f.next++
			id = fmt.Sprintf("%s-%d", kind, f.next)
		}
	}
	e["id"] = id
	if f.Objects[kind] == nil {
		f.Objects[kind] = map[string]Entity{}
	}
	f.Objects[kind][id] = e
	return id
}

var term = regexp.MustCompile(`^\s*([A-Za-z_.-]+)\s*(=|in)\s*(.+?)\s*$`)

func match(e Entity, filter string) (bool, error) {
	if filter == "" {
		return true, nil
	}
	for t := range strings.SplitSeq(filter, " and ") {
		m := term.FindStringSubmatch(t)
		if m == nil {
			return false, &APIError{Status: http.StatusBadRequest, Code: "INVALID_FILTER", Message: t}
		}
		var want []string
		if m[2] == "=" {
			m[3] = "[" + m[3] + "]"
		}
		if err := json.Unmarshal([]byte(m[3]), &want); err != nil {
			return false, &APIError{Status: http.StatusBadRequest, Code: "INVALID_FILTER", Message: t}
		}
		var got = e[m[1]]
		if k, ok := strings.CutPrefix(m[1], "tags."); ok {
			got = e.Tags()[k]
		}
		s, _ := got.(string)
		found := false
		for _, w := range want {
			found = found || s == w
		}
		if !found {
			return false, nil
		}
	}
	return true, nil
}

func (f *Fake) List(_ context.Context, kind Kind, filter string) ([]Entity, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.Calls = append(f.Calls, "list "+string(kind))
	var out []Entity
	for _, e := range f.Objects[kind] {
		ok, err := match(e, filter)
		if err != nil {
			return nil, err
		}
		if ok {
			c := clone(e)
			switch kind {
			case Identities:
				stripEnrollmentSecrets(c)
			case EdgeRouters:
				stripRouterSecrets(c)
			}
			out = append(out, c)
		}
	}
	return out, nil
}

func (f *Fake) nameTaken(kind Kind, name, except string) bool {
	if name == "" {
		return false
	}
	for id, e := range f.Objects[kind] {
		if id != except && e.Name() == name {
			return true
		}
	}
	return false
}

func (f *Fake) Create(_ context.Context, kind Kind, body Entity) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.Calls = append(f.Calls, "create "+string(kind)+" "+body.Name())
	if f.nameTaken(kind, body.Name(), "") {
		return "", &APIError{Status: http.StatusBadRequest, Code: "COULD_NOT_VALIDATE", Message: "name must be unique"}
	}
	b := clone(body)
	delete(b, "id")
	f.refsAsObjects(kind, b)
	if kind == Enrollments {
		iid, _ := b["identityId"].(string)
		for _, e := range f.Objects[Enrollments] {
			if e["identity"] == iid {
				return "", &APIError{Status: http.StatusBadRequest, Code: "ENROLLMENT_EXISTS", Message: "enrollment of same method exists"}
			}
		}
		b["identity"] = iid
		f.next++
		b["jwt"] = fmt.Sprintf("jwt-%d", f.next)
	}
	if kind == EdgeRouters {
		f.next++
		b["enrollmentJwt"] = fmt.Sprintf("router-jwt-%d", f.next)
		b["enrollmentExpiresAt"] = time.Now().Add(3 * time.Hour).UTC().Format(time.RFC3339)
		b["isVerified"] = false
		b["isOnline"] = false
	}
	if kind == CertificateAuthorities {
		f.next++
		b["verificationToken"] = fmt.Sprintf("tok-%d", f.next)
		b["isVerified"] = false
		b["fingerprint"] = certFingerprint(fmt.Sprint(b["certPem"]))
	}
	id := f.put(kind, "", b)
	if _, ott := clone(body)["enrollment"].(map[string]any); ott && kind == Identities {
		f.next++
		f.put(Enrollments, "", Entity{"identity": id, "jwt": fmt.Sprintf("jwt-%d", f.next), "expiresAt": time.Now().Add(3 * time.Hour).UTC().Format(time.RFC3339)})
	}
	return id, nil
}

func (f *Fake) Update(_ context.Context, kind Kind, id string, body Entity) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.Calls = append(f.Calls, "update "+string(kind)+" "+body.Name())
	if _, ok := f.Objects[kind][id]; !ok {
		return &APIError{Status: http.StatusNotFound, Code: "NOT_FOUND"}
	}
	if f.nameTaken(kind, body.Name(), id) {
		return &APIError{Status: http.StatusBadRequest, Code: "COULD_NOT_VALIDATE", Message: "name must be unique"}
	}
	next := clone(body)
	f.refsAsObjects(kind, next)
	// Ziti keeps these fields on an update. A CA also keeps its certificate: a new certPem is silently ignored.
	keep := map[Kind][]string{
		CertificateAuthorities: {"certPem", "fingerprint", "verificationToken", "isVerified"},
		EdgeRouters:            {"enrollmentJwt", "enrollmentExpiresAt", "isVerified", "isOnline"},
	}
	cur := f.Objects[kind][id]
	for _, k := range keep[kind] {
		if v, ok := cur[k]; ok {
			next[k] = v
		}
	}
	f.put(kind, id, next)
	return nil
}

// refsAsObjects makes the service and router of a terminator look like Ziti returns them: an object with id and name.
func (f *Fake) refsAsObjects(kind Kind, b Entity) {
	if kind != Terminators {
		return
	}
	for key, from := range map[string]Kind{"service": Services, "router": EdgeRouters} {
		if id, ok := b[key].(string); ok {
			b[key] = map[string]any{"id": id, "name": f.Objects[from][id].Name()}
		}
	}
}

func certFingerprint(certPEM string) string {
	sum := sha1.Sum([]byte(certPEM))
	if block, _ := pem.Decode([]byte(certPEM)); block != nil {
		sum = sha1.Sum(block.Bytes)
	}
	return hex.EncodeToString(sum[:])
}

func (f *Fake) Enrollment(_ context.Context, kind Kind, id string) (string, time.Time, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.Calls = append(f.Calls, "enrollment "+string(kind)+" "+id)
	e, ok := f.Objects[kind][id]
	if !ok {
		return "", time.Time{}, &APIError{Status: http.StatusNotFound, Code: "NOT_FOUND"}
	}
	jwt, _ := e["enrollmentJwt"].(string)
	exp, _ := time.Parse(time.RFC3339, fmt.Sprint(e["enrollmentExpiresAt"]))
	return jwt, exp, nil
}

func (f *Fake) ReEnroll(_ context.Context, kind Kind, id string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.Calls = append(f.Calls, "re-enroll "+string(kind)+" "+id)
	e, ok := f.Objects[kind][id]
	if !ok {
		return &APIError{Status: http.StatusNotFound, Code: "NOT_FOUND"}
	}
	f.next++
	e["enrollmentJwt"] = fmt.Sprintf("router-jwt-%d", f.next)
	e["enrollmentExpiresAt"] = time.Now().Add(3 * time.Hour).UTC().Format(time.RFC3339)
	return nil
}

// Verify accepts a certificate whose common name is the verification token. It does not check the signature.
func (f *Fake) Verify(_ context.Context, kind Kind, id string, pemCertificate string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.Calls = append(f.Calls, "verify "+string(kind)+" "+id)
	ca, ok := f.Objects[kind][id]
	if !ok {
		return &APIError{Status: http.StatusNotFound, Code: "NOT_FOUND"}
	}
	block, _ := pem.Decode([]byte(pemCertificate))
	if block == nil {
		return &APIError{Status: http.StatusBadRequest, Code: "COULD_NOT_VALIDATE", Message: "not a certificate"}
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil || cert.Subject.CommonName != ca["verificationToken"] {
		return &APIError{Status: http.StatusBadRequest, Code: "COULD_NOT_VALIDATE", Message: "wrong verification certificate"}
	}
	ca["isVerified"] = true
	delete(ca, "verificationToken")
	return nil
}

func (f *Fake) Patch(_ context.Context, kind Kind, id string, body Entity) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.Calls = append(f.Calls, "patch "+string(kind)+" "+id)
	cur, ok := f.Objects[kind][id]
	if !ok {
		return &APIError{Status: http.StatusNotFound, Code: "NOT_FOUND"}
	}
	for k, v := range clone(body) {
		cur[k] = v
	}
	return nil
}

func (f *Fake) Delete(_ context.Context, kind Kind, id string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.Calls = append(f.Calls, "delete "+string(kind)+" "+id)
	delete(f.Objects[kind], id)
	return nil
}

func clone(e Entity) Entity {
	b, _ := json.Marshal(e)
	var c Entity
	_ = json.Unmarshal(b, &c)
	return c
}

func mustJSON(v any) []byte {
	b, _ := json.Marshal(v)
	return b
}

func (f *Fake) Version(context.Context) (string, error) { return f.VersionString, nil }
