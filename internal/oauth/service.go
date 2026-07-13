package oauth

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/davidvanlaatum/inventree-mcp/internal/platform"
	"github.com/modelcontextprotocol/go-sdk/auth"
)

type CredentialValidator interface {
	ValidateCredential(context.Context, Credential) error
}

type Service struct {
	Codec               EnvelopeCodec
	MetadataFetcher     ClientMetadataFetcher
	CodeStore           *CodeStore
	Clock               platform.Clock
	IDGenerator         platform.IDGenerator
	CredentialValidator CredentialValidator
	AccessLifetime      time.Duration
	RefreshLifetime     time.Duration
	SessionLifetime     time.Duration
	CodeLifetime        time.Duration
}

type AuthorizationRequest struct {
	Issuer        string
	Audience      string
	Subject       string
	ClientID      string
	RedirectURI   string
	PKCEChallenge string
	Scopes        []string
	Credential    Credential
}

type TokenPair struct {
	AccessToken      string
	RefreshToken     string
	AccessExpiresAt  time.Time
	RefreshExpiresAt time.Time
	SessionExpiresAt time.Time
}

func (s Service) IssueAuthorizationCode(ctx context.Context, req AuthorizationRequest) (string, error) {
	if req.Issuer == "" || req.Audience == "" || req.Subject == "" || req.PKCEChallenge == "" {
		return "", errors.New("authorization request is incomplete")
	}
	if err := req.Credential.Validate(); err != nil {
		return "", err
	}
	if _, err := s.MetadataFetcher.FetchAndValidate(ctx, req.ClientID, req.RedirectURI); err != nil {
		return "", err
	}
	store := s.CodeStore
	if store == nil {
		return "", errors.New("authorization code store is required")
	}
	idGenerator := s.IDGenerator
	if idGenerator == nil {
		idGenerator = platform.RandomIDGenerator{}
	}
	codeID, err := idGenerator.NewID(ctx)
	if err != nil {
		return "", err
	}
	now := s.now()
	expiresAt := now.Add(defaultDuration(s.CodeLifetime, DefaultCodeLifetime))
	claims := AuthorizationCodeClaims{
		Type:                TokenTypeCode,
		CodeID:              codeID,
		Issuer:              req.Issuer,
		Audience:            req.Audience,
		Subject:             req.Subject,
		ClientID:            req.ClientID,
		RedirectURI:         req.RedirectURI,
		PKCEChallenge:       req.PKCEChallenge,
		PKCEChallengeMethod: "S256",
		Scopes:              append([]string(nil), req.Scopes...),
		IssuedAt:            now,
		ExpiresAt:           expiresAt,
		Credential:          req.Credential,
	}
	code, err := s.Codec.Seal(ctx, AssociatedData{
		Issuer:   req.Issuer,
		Audience: req.Audience,
		ClientID: req.ClientID,
		Type:     TokenTypeCode,
	}, claims)
	if err != nil {
		return "", err
	}
	if err := store.Store(codeID, expiresAt); err != nil {
		return "", err
	}
	return code, nil
}

func (s Service) ExchangeAuthorizationCode(ctx context.Context, code string, aad AssociatedData, redirectURI string, pkceVerifier string) (TokenPair, error) {
	var claims AuthorizationCodeClaims
	if err := s.Codec.Open(code, AssociatedData{
		Issuer:   aad.Issuer,
		Audience: aad.Audience,
		ClientID: aad.ClientID,
		Type:     TokenTypeCode,
	}, &claims); err != nil {
		return TokenPair{}, ErrInvalidCode
	}
	if err := claims.validateForUse(s.now(), aad, redirectURI, pkceVerifier); err != nil {
		return TokenPair{}, err
	}
	if s.CodeStore == nil {
		return TokenPair{}, errors.New("authorization code store is required")
	}
	if err := s.CodeStore.Consume(claims.CodeID); err != nil {
		return TokenPair{}, err
	}
	return s.issuePair(ctx, TokenClaims{
		Issuer:     claims.Issuer,
		Audience:   claims.Audience,
		Subject:    claims.Subject,
		ClientID:   claims.ClientID,
		Scopes:     claims.Scopes,
		Credential: claims.Credential,
	}, s.now().Add(defaultDuration(s.SessionLifetime, DefaultSessionLifetime)))
}

func (s Service) Refresh(ctx context.Context, refreshToken string, aad AssociatedData) (TokenPair, error) {
	var claims TokenClaims
	if err := s.Codec.Open(refreshToken, AssociatedData{
		Issuer:   aad.Issuer,
		Audience: aad.Audience,
		ClientID: aad.ClientID,
		Type:     TokenTypeRefresh,
	}, &claims); err != nil {
		return TokenPair{}, ErrInvalidToken
	}
	if err := claims.validateForUse(s.now(), TokenTypeRefresh, aad); err != nil {
		return TokenPair{}, err
	}
	if s.CredentialValidator != nil {
		if err := s.CredentialValidator.ValidateCredential(ctx, claims.Credential); err != nil {
			return TokenPair{}, err
		}
	}
	return s.issuePair(ctx, TokenClaims{
		Issuer:     claims.Issuer,
		Audience:   claims.Audience,
		Subject:    claims.Subject,
		ClientID:   claims.ClientID,
		Scopes:     claims.Scopes,
		Credential: claims.Credential,
	}, claims.SessionExpiresAt)
}

func AccessTokenVerifier(codec EnvelopeCodec, issuer string, audience string, clientIDs []string, clock platform.Clock) auth.TokenVerifier {
	return func(_ context.Context, token string, _ *http.Request) (*auth.TokenInfo, error) {
		now := time.Now()
		if clock != nil {
			now = clock.Now()
		}
		for _, clientID := range clientIDs {
			var claims TokenClaims
			aad := AssociatedData{
				Issuer:   issuer,
				Audience: audience,
				ClientID: clientID,
				Type:     TokenTypeAccess,
			}
			if err := codec.Open(token, aad, &claims); err != nil {
				continue
			}
			if err := claims.validateForUse(now, TokenTypeAccess, aad); err != nil {
				return nil, auth.ErrInvalidToken
			}
			return TokenInfoWithCredential(&auth.TokenInfo{
				Scopes:     append([]string(nil), claims.Scopes...),
				Expiration: claims.ExpiresAt,
				UserID:     claims.Subject,
			}, claims.Credential), nil
		}
		return nil, auth.ErrInvalidToken
	}
}

func (s Service) issuePair(ctx context.Context, base TokenClaims, sessionExpiresAt time.Time) (TokenPair, error) {
	now := s.now()
	accessExpiresAt := now.Add(defaultDuration(s.AccessLifetime, DefaultAccessTokenLifetime))
	refreshExpiresAt := now.Add(defaultDuration(s.RefreshLifetime, DefaultRefreshTokenLifetime))
	if sessionExpiresAt.Before(refreshExpiresAt) {
		refreshExpiresAt = sessionExpiresAt
	}
	if sessionExpiresAt.Before(accessExpiresAt) {
		accessExpiresAt = sessionExpiresAt
	}
	accessClaims := base
	accessClaims.Type = TokenTypeAccess
	accessClaims.IssuedAt = now
	accessClaims.ExpiresAt = accessExpiresAt
	accessClaims.SessionExpiresAt = sessionExpiresAt
	refreshClaims := base
	refreshClaims.Type = TokenTypeRefresh
	refreshClaims.IssuedAt = now
	refreshClaims.ExpiresAt = refreshExpiresAt
	refreshClaims.SessionExpiresAt = sessionExpiresAt
	accessToken, err := s.Codec.Seal(ctx, AssociatedData{
		Issuer:   base.Issuer,
		Audience: base.Audience,
		ClientID: base.ClientID,
		Type:     TokenTypeAccess,
	}, accessClaims)
	if err != nil {
		return TokenPair{}, err
	}
	refreshToken, err := s.Codec.Seal(ctx, AssociatedData{
		Issuer:   base.Issuer,
		Audience: base.Audience,
		ClientID: base.ClientID,
		Type:     TokenTypeRefresh,
	}, refreshClaims)
	if err != nil {
		return TokenPair{}, err
	}
	return TokenPair{
		AccessToken:      accessToken,
		RefreshToken:     refreshToken,
		AccessExpiresAt:  accessExpiresAt,
		RefreshExpiresAt: refreshExpiresAt,
		SessionExpiresAt: sessionExpiresAt,
	}, nil
}

func (s Service) now() time.Time {
	if s.Clock == nil {
		return time.Now()
	}
	return s.Clock.Now()
}
