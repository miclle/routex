package saml

import (
	"regexp"
	"time"

	coresaml "github.com/crewjam/saml"
)

var timestampPattern = regexp.MustCompile(`^[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9]{2}:[0-9]{2}:[0-9]{2}(\.[0-9]{1,9})?(Z|[+-](0[0-9]|1[0-9]|2[0-3]):[0-5][0-9])$`)

type proof struct {
	identity           Identity
	issue              time.Time
	notBefore          time.Time
	conditionsExpiry   time.Time
	confirmationBefore time.Time
	confirmationExpiry time.Time
	authnInstant       time.Time
	sessionExpiry      time.Time
	contextClass       string
	nameQualifier      string
	spQualifier        string
	audienceCount      int
	signedParent       string
}

func (c *Client) profile(root *element, requestID string, now time.Time) (proof, error) {
	if !root.named(protocolNS, "Response") || !root.attrs("ID", "Version", "IssueInstant", "Destination", "InResponseTo") || !root.container() || !xmlID(root.attr("ID"), 1) || root.attr("Version") != "2.0" || root.attr("Destination") != c.acs.String() || root.attr("InResponseTo") != requestID {
		return proof{}, ErrProtocol
	}
	if _, ok := issueTime(root.attr("IssueInstant"), now); !ok {
		return proof{}, ErrProtocol
	}
	for _, child := range root.children {
		if !child.named(assertionNS, "Issuer") && !child.named(protocolNS, "Status") && !child.named(assertionNS, "Assertion") && !child.named(signatureNS, "Signature") {
			return proof{}, ErrProtocol
		}
	}
	issuer, e := root.only(assertionNS, "Issuer")
	if e != nil || !issuerIs(issuer, c.issuer) {
		return proof{}, ErrProtocol
	}
	status, e := root.only(protocolNS, "Status")
	if e != nil || !status.attrs() || !status.container() || len(status.children) != 1 {
		return proof{}, ErrProtocol
	}
	code, e := status.only(protocolNS, "StatusCode")
	if e != nil || !code.attrs("Value") || !code.simple() || !code.container() || code.attr("Value") != successStatus {
		return proof{}, ErrProtocol
	}
	assertion, e := root.only(assertionNS, "Assertion")
	if e != nil {
		return proof{}, ErrProtocol
	}
	signatures := []*element{}
	assertions := 0
	var walk func(*element)
	walk = func(n *element) {
		if n.name.Local == "Assertion" {
			assertions++
		}
		if n.name.Local == "Signature" {
			signatures = append(signatures, n)
		}
		for _, child := range n.children {
			walk(child)
		}
	}
	walk(root)
	if assertions != 1 || len(signatures) != 1 || (signatures[0].parent != root && signatures[0].parent != assertion) {
		return proof{}, ErrProtocol
	}
	if err := c.signatureProfile(signatures[0]); err != nil {
		return proof{}, ErrProtocol
	}
	result, err := c.assertionProfile(assertion, requestID, now)
	if err != nil {
		return proof{}, ErrProtocol
	}
	result.signedParent = signatures[0].parent.name.Local
	return result, nil
}

func (c *Client) assertionProfile(a *element, requestID string, now time.Time) (proof, error) {
	if !a.named(assertionNS, "Assertion") || !a.attrs("ID", "Version", "IssueInstant") || !xmlID(a.attr("ID"), 1) || a.attr("Version") != "2.0" || !childrenAllowed(a, assertionNS, "Issuer", "Signature", "Subject", "Conditions", "AuthnStatement", "AttributeStatement") {
		return proof{}, ErrProtocol
	}
	issue, ok := issueTime(a.attr("IssueInstant"), now)
	if !ok {
		return proof{}, ErrProtocol
	}
	issuer, e := a.only(assertionNS, "Issuer")
	if e != nil || !issuerIs(issuer, c.issuer) {
		return proof{}, ErrProtocol
	}
	subject, e := a.only(assertionNS, "Subject")
	if e != nil || !subject.attrs() || !childrenAllowed(subject, assertionNS, "NameID", "SubjectConfirmation") || len(subject.children) != 2 {
		return proof{}, ErrProtocol
	}
	name, e := subject.only(assertionNS, "NameID")
	if e != nil || !name.attrs("Format", "NameQualifier", "SPNameQualifier") || !name.simple() || name.attr("Format") != persistentNameID || !text(name.value.String(), 256) {
		return proof{}, ErrProtocol
	}
	if name.attr("NameQualifier") != "" && name.attr("NameQualifier") != c.issuer || name.attr("SPNameQualifier") != "" && name.attr("SPNameQualifier") != c.entity {
		return proof{}, ErrProtocol
	}
	confirmation, e := subject.only(assertionNS, "SubjectConfirmation")
	if e != nil || !confirmation.attrs("Method") || confirmation.attr("Method") != bearerMethod || !childrenAllowed(confirmation, assertionNS, "SubjectConfirmationData") || len(confirmation.children) != 1 {
		return proof{}, ErrProtocol
	}
	data, e := confirmation.only(assertionNS, "SubjectConfirmationData")
	if e != nil || !data.attrs("Recipient", "InResponseTo", "NotBefore", "NotOnOrAfter") || !data.simple() || !data.container() || data.attr("Recipient") != c.acs.String() || data.attr("InResponseTo") != requestID {
		return proof{}, ErrProtocol
	}
	confirmationExpiry, ok := expiryTime(data.attr("NotOnOrAfter"), now)
	if !ok {
		return proof{}, ErrProtocol
	}
	var confirmationBefore time.Time
	if data.attr("NotBefore") != "" {
		confirmationBefore, ok = parseTime(data.attr("NotBefore"))
		if !ok || confirmationBefore.After(now) || !confirmationBefore.Before(confirmationExpiry) {
			return proof{}, ErrProtocol
		}
	}
	conditions, e := a.only(assertionNS, "Conditions")
	if e != nil || !conditions.attrs("NotBefore", "NotOnOrAfter") || !childrenAllowed(conditions, assertionNS, "AudienceRestriction") {
		return proof{}, ErrProtocol
	}
	notBefore, ok := parseTime(conditions.attr("NotBefore"))
	if !ok || notBefore.After(now) {
		return proof{}, ErrProtocol
	}
	conditionsExpiry, ok := expiryTime(conditions.attr("NotOnOrAfter"), now)
	if !ok || !notBefore.Before(conditionsExpiry) || conditionsExpiry.Sub(notBefore) > maxLifetime {
		return proof{}, ErrProtocol
	}
	audiences := conditions.childrenNamed(assertionNS, "AudienceRestriction")
	if len(audiences) == 0 || len(audiences) > 8 {
		return proof{}, ErrProtocol
	}
	// This finite profile admits exactly one Audience per restriction. ALL
	// restrictions must match; no missing/default audience or OR across groups.
	for _, restriction := range audiences {
		if !restriction.attrs() || !childrenAllowed(restriction, assertionNS, "Audience") || len(restriction.children) != 1 {
			return proof{}, ErrProtocol
		}
		audience, e := restriction.only(assertionNS, "Audience")
		if e != nil || !audience.attrs() || !audience.simple() || audience.value.String() != c.entity {
			return proof{}, ErrProtocol
		}
	}
	authn, e := a.only(assertionNS, "AuthnStatement")
	if e != nil || !authn.attrs("AuthnInstant", "SessionIndex", "SessionNotOnOrAfter") || !childrenAllowed(authn, assertionNS, "AuthnContext") || len(authn.children) != 1 || len(authn.attr("SessionIndex")) > 256 {
		return proof{}, ErrProtocol
	}
	authnInstant, ok := parseTime(authn.attr("AuthnInstant"))
	if !ok || authnInstant.After(now) {
		return proof{}, ErrProtocol
	}
	authnContext, e := authn.only(assertionNS, "AuthnContext")
	if e != nil || !authnContext.attrs() || !childrenAllowed(authnContext, assertionNS, "AuthnContextClassRef") || len(authnContext.children) != 1 {
		return proof{}, ErrProtocol
	}
	class, e := authnContext.only(assertionNS, "AuthnContextClassRef")
	if e != nil || !class.attrs() || !class.simple() || !identifier(class.value.String()) {
		return proof{}, ErrProtocol
	}
	expiry := conditionsExpiry
	if confirmationExpiry.Before(expiry) {
		expiry = confirmationExpiry
	}
	var sessionExpiry time.Time
	if authn.attr("SessionNotOnOrAfter") != "" {
		sessionExpiry, ok = parseTime(authn.attr("SessionNotOnOrAfter"))
		if !ok || !now.Before(sessionExpiry) {
			return proof{}, ErrProtocol
		}
		if sessionExpiry.Before(expiry) {
			expiry = sessionExpiry
		}
	}
	// Optional external attributes remain signed but confer no identity/rights.
	// Reject nested protocol/security-bearing structures, including hidden IDs.
	for _, statement := range a.childrenNamed(assertionNS, "AttributeStatement") {
		if !statement.attrs() || !childrenAllowed(statement, assertionNS, "Attribute") || len(statement.children) > 64 {
			return proof{}, ErrProtocol
		}
		for _, attribute := range statement.children {
			if !attribute.attrs("Name", "NameFormat", "FriendlyName") || !text(attribute.attr("Name"), 256) || !childrenAllowed(attribute, assertionNS, "AttributeValue") || len(attribute.children) > 16 {
				return proof{}, ErrProtocol
			}
			for _, value := range attribute.children {
				if !value.attrs() || !value.simple() {
					return proof{}, ErrProtocol
				}
			}
		}
	}
	if len(a.childrenNamed(assertionNS, "AttributeStatement")) > 1 {
		return proof{}, ErrProtocol
	}
	return proof{identity: Identity{Issuer: c.issuer, Subject: name.value.String(), RequestID: requestID, AssertionID: a.attr("ID"), ExpiresAt: expiry.UTC()}, issue: issue, notBefore: notBefore, conditionsExpiry: conditionsExpiry, confirmationBefore: confirmationBefore, confirmationExpiry: confirmationExpiry, authnInstant: authnInstant, sessionExpiry: sessionExpiry, contextClass: class.value.String(), nameQualifier: name.attr("NameQualifier"), spQualifier: name.attr("SPNameQualifier"), audienceCount: len(audiences)}, nil
}
func issuerIs(e *element, expected string) bool {
	return e.attrs("Format") && (e.attr("Format") == "" || e.attr("Format") == entityFormat) && e.simple() && e.value.String() == expected
}
func parseTime(value string) (time.Time, bool) {
	if len(value) > 64 || !timestampPattern.MatchString(value) {
		return time.Time{}, false
	}
	t, e := time.Parse(time.RFC3339Nano, value)
	return t.UTC(), e == nil && !t.IsZero() && t.Year() > 0
}
func issueTime(value string, now time.Time) (time.Time, bool) {
	t, ok := parseTime(value)
	return t, ok && !t.After(now) && now.Sub(t) <= maxIssueAge
}
func expiryTime(value string, now time.Time) (time.Time, bool) {
	t, ok := parseTime(value)
	return t, ok && now.Before(t) && !t.After(now.Add(maxLifetime))
}

func (p proof) matches(a *coresaml.Assertion) bool {
	if a == nil || a.ID != p.identity.AssertionID || a.Version != "2.0" || !a.IssueInstant.Equal(p.issue) || a.Issuer.Value != p.identity.Issuer || a.Subject == nil || a.Subject.NameID == nil || a.Conditions == nil || len(a.Subject.SubjectConfirmations) != 1 || len(a.AuthnStatements) != 1 {
		return false
	}
	name := a.Subject.NameID
	confirmation := a.Subject.SubjectConfirmations[0]
	data := confirmation.SubjectConfirmationData
	authn := a.AuthnStatements[0]
	if name.Value != p.identity.Subject || name.Format != persistentNameID || name.NameQualifier != p.nameQualifier || name.SPNameQualifier != p.spQualifier || name.SPProvidedID != "" || confirmation.Method != bearerMethod || data == nil || data.InResponseTo != p.identity.RequestID || !data.NotBefore.Equal(p.confirmationBefore) || !data.NotOnOrAfter.Equal(p.confirmationExpiry) || !a.Conditions.NotBefore.Equal(p.notBefore) || !a.Conditions.NotOnOrAfter.Equal(p.conditionsExpiry) || len(a.Conditions.AudienceRestrictions) != p.audienceCount || !authn.AuthnInstant.Equal(p.authnInstant) || authn.AuthnContext.AuthnContextClassRef == nil || authn.AuthnContext.AuthnContextClassRef.Value != p.contextClass {
		return false
	}
	if p.sessionExpiry.IsZero() {
		return authn.SessionNotOnOrAfter == nil
	}
	return authn.SessionNotOnOrAfter != nil && authn.SessionNotOnOrAfter.Equal(p.sessionExpiry)
}
