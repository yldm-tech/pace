package auth

import "fmt"

const (
	ErrorInstanceNotConfigured               = 5000
	ErrorInvalidEmail                        = 5005
	ErrorEmailRequired                       = 5010
	ErrorSignupDisabled                      = 5015
	ErrorMagicLinkLoginDisabled              = 5016
	ErrorBotUserLoginForbidden               = 5017
	ErrorPasswordLoginDisabled               = 5018
	ErrorUserAccountDeactivated              = 5019
	ErrorInvalidPassword                     = 5020
	ErrorPasswordTooWeak                     = 5021
	ErrorSMTPNotConfigured                   = 5025
	ErrorUserAlreadyExists                   = 5030
	ErrorAuthenticationFailedSignUp          = 5035
	ErrorRequiredEmailPasswordSignUp         = 5040
	ErrorInvalidEmailSignUp                  = 5045
	ErrorInvalidEmailMagicSignUp             = 5050
	ErrorMagicSignUpEmailCodeRequired        = 5055
	ErrorEmailPasswordAuthenticationDisabled = 5056
	ErrorUserDoesNotExist                    = 5060
	ErrorAuthenticationFailedSignIn          = 5065
	ErrorRequiredEmailPasswordSignIn         = 5070
	ErrorInvalidEmailSignIn                  = 5075
	ErrorInvalidEmailMagicSignIn             = 5080
	ErrorMagicSignInEmailCodeRequired        = 5085
	ErrorInvalidMagicCodeSignIn              = 5090
	ErrorInvalidMagicCodeSignUp              = 5092
	ErrorExpiredMagicCodeSignIn              = 5095
	ErrorExpiredMagicCodeSignUp              = 5097
	ErrorEmailCodeAttemptExhaustedSignIn     = 5100
	ErrorEmailCodeAttemptExhaustedSignUp     = 5102
	ErrorOAuthNotConfigured                  = 5104
	ErrorGoogleNotConfigured                 = 5105
	ErrorGitHubNotConfigured                 = 5110
	ErrorGitLabNotConfigured                 = 5111
	ErrorGiteaNotConfigured                  = 5112
	ErrorGoogleOAuthProvider                 = 5115
	ErrorGitHubOAuthProvider                 = 5120
	ErrorGitLabOAuthProvider                 = 5121
	ErrorGitHubUserNotInOrg                  = 5122
	ErrorGiteaOAuthProvider                  = 5123
	ErrorOAuthProviderUnverifiedEmail        = 5124
	ErrorInvalidPasswordToken                = 5125
	ErrorExpiredPasswordToken                = 5130
	ErrorIncorrectOldPassword                = 5135
	ErrorMissingPassword                     = 5138
	ErrorInvalidNewPassword                  = 5140
	ErrorPasswordAlreadySet                  = 5145
	ErrorRateLimitExceeded                   = 5900
	ErrorAuthenticationFailed                = 5999
)

type Error struct {
	Code    int
	Message string
	Payload map[string]any
}

func (e *Error) Error() string {
	return fmt.Sprintf("authentication error %d: %s", e.Code, e.Message)
}

func (e *Error) Response() map[string]any {
	response := map[string]any{"error_code": e.Code, "error_message": e.Message}
	for key, value := range e.Payload {
		response[key] = value
	}
	return response
}

func authError(code int, message string, payload map[string]any) *Error {
	return &Error{Code: code, Message: message, Payload: payload}
}
