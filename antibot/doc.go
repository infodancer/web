// Package antibot is the anti-abuse check for public intake forms: newsletter
// signups, contact forms, resume requests -- anything an anonymous visitor can
// submit that costs the site a database row, an email, or a person's attention.
//
// A Guard checks a request the handler has already parsed and returns a
// Verdict. It never writes the response: each form keeps its own copy, and the
// verdict only says whether the request is allowed and, if not, why.
//
//	g, err := antibot.New(antibot.Config{DB: pool, Secret: key, Proxies: proxies})
//
//	// Rendering the form:
//	data.AntibotToken = g.Token("contact")
//
//	// Handling the submission, after ParseForm:
//	v, err := g.Check(ctx, r, "contact", antibot.Email(addr))
//	switch {
//	case err != nil:  // store failure; the form's own write would fail too
//	case v.Silent:    // honeypot: render a fake success, never tell the bot
//	case !v.Allowed:  // render this form's rejection for v.Reason
//	}
//
// Check runs its checks cheapest first, so a request rejected early costs
// nothing later: the honeypot, then the signed timing token, then the per-IP
// limit, then the email's syntax, its per-address limit, and finally an MX
// lookup. A bot without a valid token never reaches the database, and a
// rate-limited request never makes a DNS query.
//
// What each check defends against:
//
//   - Honeypot: a hidden field no person fills in. A filled one is answered with
//     silence, so a bot learns nothing about why it failed.
//   - Timing token: HMAC-SHA256 over the form name and the time the form was
//     rendered. A submission faster than a person could type, older than the
//     maximum age, issued for another form, or carrying a timestamp the server
//     did not sign is rejected. The signature is what makes backdating useless.
//   - Rate limits: counted per form in Postgres, so a restart or a deploy does
//     not hand an attacker a fresh budget. The IP comes from httplog.Proxies, so
//     X-Forwarded-For is believed only from a configured proxy. An IPv6 address
//     is counted by its /64, since one subscriber usually holds all of it. The
//     email limit catches one address submitted from many IPs.
//   - MX: an address whose domain accepts no mail is rejected. A resolver
//     failure is not evidence of a bot, so it lets the request through.
//
// The limiter stores an HMAC of each address and email, never the value. It
// needs equality, not identity, so its table identifies nobody.
//
// A store error fails closed: Check returns the error and a rejected verdict.
// Every consumer writes its submission to the same database, so a form whose
// limiter cannot reach it could not have saved anyway.
package antibot
