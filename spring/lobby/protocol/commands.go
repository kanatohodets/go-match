package protocol

type JoinQueueRequest struct {
	UserNames []string `json:"userNames"`
	Name      string   `json:"name"`
}

type JoinQueueAccept struct {
	UserNames []string `json:"userNames"`
	Name      string   `json:"name"`
}

type JoinQueueDeny struct {
	UserNames []string `json:"userNames"`
	Name      string   `json:"name"`
	Reason    string   `json:"reason"`
}

type ReadyCheck struct {
	UserNames    []string `json:"userNames"`
	Name         string   `json:"name"`
	ResponseTime int      `json:"responseTime"`
}

type ReadyCheckResponse struct {
	UserName     string `json:"userName"`
	Name         string `json:"name"`
	Response     string `json:"response"`
	ResponseTime int    `json:"responseTime"`
}

type ReadyCheckResult struct {
	UserNames []string `json:"userNames"`
	Name      string   `json:"name"`
	Result    string   `json:"result"`
}

type ConnectUser struct {
	UserName string `json:"userName"`
	IP       string `json:"ip"`
	Port     string `json:"port"`
	Password string `json:"password"`
	Engine   string `json:"engine"`
}

type OpenQueue struct {
	Name string `json:"name"`
}

type QueueLeft struct {
	UserNames []string `json:"userNames"`
	Name      string   `json:"name"`
}

type CloseQueue struct {
	Name string `json:"name"`
}
