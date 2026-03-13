package core

type Config struct {
	CookieFilePath   string  `json:"cookie_file_path"`
	TargetURL        string  `json:"target_url"`
	InputName        string  `json:"input_name"`
	Longitude        float64 `json:"longitude"`
	Latitude         float64 `json:"latitude"`
	FormattedAddress string  `json:"formatted_address"`
	UserAgent        string  `json:"user_agent"`
	Locale           string  `json:"locale"`
	AcceptLanguage   string  `json:"accept_language"`
	VerifyCookies    string  `json:"verify_cookies"`
}

type Cookie struct {
	Name     string  `json:"name"`
	Value    string  `json:"value"`
	Domain   string  `json:"domain"`
	Path     string  `json:"path"`
	Expires  float64 `json:"expires"`
	HTTPOnly bool    `json:"httpOnly"`
	Secure   bool    `json:"secure"`
	SameSite string  `json:"sameSite"`
}

type CookieData struct {
	Cookies []Cookie `json:"cookies"`
}

type AuthResponse struct {
	UserID   any    `json:"userid"`
	Nickname string `json:"nickname"`
}

type FormResponse struct {
	Code int `json:"code"`
	Data struct {
		Name        string `json:"name"`
		QuestionMap map[string]struct {
			Type string `json:"type"`
		} `json:"questionMap"`
		Setting struct {
			BaseSetting struct {
				CommitConfig struct {
					Options []struct {
						ID   string `json:"id"`
						Text string `json:"text"`
					} `json:"options"`
				} `json:"commitConfig"`
			} `json:"baseSetting"`
		} `json:"setting"`
	} `json:"data"`
}

type AnswersResponse struct {
	Code int `json:"code"`
	Data struct {
		Answers []struct {
			Aid string `json:"aid"`
		} `json:"answers"`
	} `json:"data"`
}

type GenericResponse struct {
	Code   int    `json:"code"`
	Result string `json:"result"`
}

type ClockInResult struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
}
