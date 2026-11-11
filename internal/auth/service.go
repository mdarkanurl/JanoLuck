package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"math/big"
	"strconv"
	"time"

	emailpkg "github.com/mdarkanurl/JanoLuck/internal/email"
	"github.com/mdarkanurl/JanoLuck/internal/kafka"
	"golang.org/x/crypto/bcrypt"
)

const kafkaPublishTimeout = 5 * time.Second

var (
	ErrInvalidInput        = errors.New("invalid input")
	ErrUserExists          = errors.New("user already exists")
	ErrVerificationPending = errors.New("verification already pending, check your mail")
	ErrInternal            = errors.New("something went wrong")
	ErrInvalidCredentials  = errors.New("invalid credentials")
	ErrTokenExpired        = errors.New("token expired")
)

type Service struct {
	repo     *Repository
	tokens   *TokenService
	producer *kafka.Producer
}

func AuthService(repo *Repository, tokens *TokenService, producer *kafka.Producer) *Service {
	return &Service{repo: repo, tokens: tokens, producer: producer}
}

func hashCode(code int) string {
	sum := sha256.Sum256(fmt.Appendf(nil, "%06d", code))
	return hex.EncodeToString(sum[:])
}

func (s *Service) SignUp(ctx context.Context, email, password string) error {
	userInDB, err := s.repo.IsUserExistInDB(email, ctx)
	if err != nil {
		return err
	}
	if userInDB {
		return ErrUserExists
	}

	userInRedis, err := s.repo.IsUserExistInRedis(email, ctx)
	if err != nil {
		return err
	}
	if userInRedis {
		return ErrVerificationPending
	}

	hashed, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return fmt.Errorf("%w: hash password: %v", ErrInternal, err)
	}

	n, err := rand.Int(rand.Reader, big.NewInt(900000))
	if err != nil {
		return fmt.Errorf("%w: generate verification code: %v", ErrInternal, err)
	}
	rawCode := int(n.Int64()) + 100000
	codeHash := hashCode(rawCode)

	if s.producer == nil {
		return fmt.Errorf("%w: kafka producer is not configured", ErrInternal)
	}

	if err := s.repo.CreateUserInRedis(ctx, email, string(hashed), codeHash); err != nil {
		return err
	}

	job := emailpkg.Job{
		To:      email,
		Subject: "verify your e-mail",
		Body:    "here is your verification code:" + strconv.Itoa(rawCode),
	}

	publishCtx, cancel := context.WithTimeout(ctx, kafkaPublishTimeout)
	defer cancel()

	if err := s.producer.Produce(publishCtx, emailpkg.VerificationTopic, job.To, job); err != nil {
		_ = s.repo.DeletePendingUser(ctx, email)
		return fmt.Errorf("%w: enqueue verification email: %v", ErrInternal, err)
	}

	return nil
}

func (s *Service) VerifyEmail(ctx context.Context, email string, code int) (*AuthTokens, error) {
	pending, err := s.repo.GetPendingUser(ctx, email)
	if err != nil {
		return nil, err
	}

	if subtle.ConstantTimeCompare([]byte(hashCode(code)), []byte(pending.VerificationCodeHash)) != 1 {
		return nil, ErrInvalidCredentials
	}

	userInDB, err := s.repo.IsUserExistInDB(email, ctx)
	if err != nil {
		return nil, err
	}
	if userInDB {
		_ = s.repo.DeletePendingUser(ctx, email)
		return nil, ErrUserExists
	}

	user, err := s.repo.CreateUserInDB(ctx, pending.Email, pending.Password)
	if err != nil {
		return nil, err
	}

	tokens, err := s.mintTokenPair(ctx, user.ID.String(), user.Email, "")
	if err != nil {
		return nil, err
	}

	if err := s.repo.DeletePendingUser(ctx, email); err != nil {
		return nil, err
	}

	return tokens, nil
}

func (s *Service) mintTokenPair(ctx context.Context, userID, email, oldRefreshHash string) (*AuthTokens, error) {
	accessToken, err := s.tokens.GenerateAccessToken(userID, email)
	if err != nil {
		return nil, err
	}

	refreshToken, err := s.tokens.GenerateRefreshToken(userID, email)
	if err != nil {
		return nil, err
	}

	if oldRefreshHash != "" {
		_ = s.repo.DeleteRefreshToken(ctx, oldRefreshHash)
	}

	if err := s.repo.StoreRefreshToken(ctx, userID, HashRefreshToken(refreshToken), s.tokens.RefreshTTL()); err != nil {
		return nil, err
	}

	return &AuthTokens{
		AccessToken:      accessToken,
		RefreshToken:     refreshToken,
		TokenType:        "Bearer",
		ExpiresIn:        int64(s.tokens.AccessTTL().Seconds()),
		RefreshExpiresIn: int64(s.tokens.RefreshTTL().Seconds()),
	}, nil
}

func (s *Service) Login(ctx context.Context, email, password string) (*AuthTokens, error) {
	user, err := s.repo.GetUserByEmail(ctx, email)
	if err != nil {
		return nil, err
	}

	if err := bcrypt.CompareHashAndPassword([]byte(user.Password), []byte(password)); err != nil {
		return nil, ErrInvalidCredentials
	}

	return s.mintTokenPair(ctx, user.ID.String(), user.Email, "")
}

func (s *Service) Refresh(ctx context.Context, refreshToken string) (*AuthTokens, error) {
	userID, _, err := s.tokens.ValidateRefreshToken(refreshToken)
	if err != nil {
		return nil, err
	}

	// TODO dont fully depend on Redis for storing refresh tokens
	oldHash := HashRefreshToken(refreshToken)
	storedUserID, err := s.repo.GetRefreshUserID(ctx, oldHash)
	if err != nil {
		return nil, err
	}
	if storedUserID != userID {
		_ = s.repo.DeleteRefreshToken(ctx, oldHash)
		return nil, ErrInvalidCredentials
	}

	user, err := s.repo.GetUserByID(ctx, userID)
	if err != nil {
		_ = s.repo.DeleteRefreshToken(ctx, oldHash)
		return nil, err
	}

	return s.mintTokenPair(ctx, user.ID.String(), user.Email, oldHash)
}

func (s *Service) Me(ctx context.Context, userID string) (string, string, error) {
	user, err := s.repo.GetUserByID(ctx, userID)
	if err != nil {
		return "", "", err
	}

	return user.ID.String(), user.Email, nil
}

func (s *Service) ForgotPassword(ctx context.Context, email string) error {
	exists, err := s.repo.IsUserExistInDB(email, ctx)
	if err != nil {
		return err
	}
	if !exists {
		return nil
	}

	if s.producer == nil {
		return fmt.Errorf("%w: kafka producer is not configured", ErrInternal)
	}

	n, err := rand.Int(rand.Reader, big.NewInt(900000))
	if err != nil {
		return fmt.Errorf("%w: generate reset code: %v", ErrInternal, err)
	}
	rawCode := int(n.Int64()) + 100000

	if err := s.repo.CreatePasswordReset(ctx, email, hashCode(rawCode)); err != nil {
		return err
	}

	job := emailpkg.Job{
		To:      email,
		Subject: "reset your password",
		Body:    "here is your password reset code:" + strconv.Itoa(rawCode),
	}

	publishCtx, cancel := context.WithTimeout(ctx, kafkaPublishTimeout)
	defer cancel()

	if err := s.producer.Produce(publishCtx, emailpkg.PasswordResetTopic, job.To, job); err != nil {
		_ = s.repo.DeletePasswordReset(ctx, email)
		return fmt.Errorf("%w: enqueue password reset email: %v", ErrInternal, err)
	}

	return nil
}

func (s *Service) ResetPassword(ctx context.Context, email string, code int, newPassword string) error {
	wantHash, err := s.repo.GetPasswordReset(ctx, email)
	if err != nil {
		return err
	}

	if subtle.ConstantTimeCompare([]byte(hashCode(code)), []byte(wantHash)) != 1 {
		return ErrInvalidCredentials
	}

	user, err := s.repo.GetUserByEmail(ctx, email)
	if err != nil {
		_ = s.repo.DeletePasswordReset(ctx, email)
		return err
	}

	hashed, err := bcrypt.GenerateFromPassword([]byte(newPassword), bcrypt.DefaultCost)
	if err != nil {
		return fmt.Errorf("%w: hash password: %v", ErrInternal, err)
	}

	if err := s.repo.UpdateUserPassword(ctx, user.ID.String(), string(hashed)); err != nil {
		return err
	}

	_ = s.repo.DeletePasswordReset(ctx, email)
	_ = s.repo.RevokeRefreshTokensForUser(ctx, user.ID.String())

	return nil
}

func (s *Service) ChangePassword(ctx context.Context, userID, userEmail, currentPassword, newPassword string) error {
	if subtle.ConstantTimeCompare([]byte(currentPassword), []byte(newPassword)) == 1 {
		return fmt.Errorf("%w: new password must be different", ErrInvalidInput)
	}

	user, err := s.repo.GetUserByEmail(ctx, userEmail)
	if err != nil {
		return err
	}

	if err := bcrypt.CompareHashAndPassword([]byte(user.Password), []byte(currentPassword)); err != nil {
		return ErrInvalidCredentials
	}

	hashed, err := bcrypt.GenerateFromPassword([]byte(newPassword), bcrypt.DefaultCost)
	if err != nil {
		return fmt.Errorf("%w: hash password: %v", ErrInternal, err)
	}

	if err := s.repo.UpdateUserPassword(ctx, user.ID.String(), string(hashed)); err != nil {
		return err
	}

	_ = s.repo.RevokeRefreshTokensForUser(ctx, user.ID.String())

	return nil
}

func (s *Service) Logout(ctx context.Context, userID string) error {
	_ = s.repo.RevokeRefreshTokensForUser(ctx, userID)
	return nil
}
