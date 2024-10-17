package app

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/birabittoh/auth-boilerplate/src/email"
)

type HabitDisplay struct {
	ID       uint
	Class    string
	Name     string
	LastAck  string
	Disabled bool
}

const (
	minUsernameLength = 3
	maxUsernameLength = 10

	classGood = "good"
	classWarn = "warn"
	classBad  = "bad"
)

var (
	validUsername  = regexp.MustCompile(`(?i)^[a-z0-9._-]+$`)
	validEmail     = regexp.MustCompile(`^[a-z0-9._%+-]+@[a-z0-9.-]+\.[a-z]{2,}$`)
	validHabitName = regexp.MustCompile(`(?i)^[a-z0-9._,\s)(-]+$`)
)

func getUserByName(username string, excluding uint) (user User, err error) {
	err = db.Model(&User{}).Where("upper(username) == upper(?) AND id != ?", username, excluding).First(&user).Error
	return
}

func sanitizeUsername(username string) (string, error) {
	if !validUsername.MatchString(username) || len(username) < minUsernameLength || len(username) > maxUsernameLength {
		return "", errors.New("invalid username")
	}

	return username, nil
}

func sanitizeEmail(email string) (string, error) {
	email = strings.ToLower(email)

	if !validEmail.MatchString(email) {
		return "", fmt.Errorf("invalid email")
	}

	return email, nil
}

func checkHabitName(name string) bool {
	return len(name) < 50 && validHabitName.MatchString(name)
}

func login(w http.ResponseWriter, userID uint, remember bool) {
	var duration time.Duration
	if remember {
		duration = durationWeek
	} else {
		duration = durationDay
	}

	cookie, err := g.GenerateCookie(duration)
	if err != nil {
		http.Error(w, "Could not generate session cookie.", http.StatusInternalServerError)
	}

	ks.Set("session:"+cookie.Value, userID, duration)
	http.SetCookie(w, cookie)
}

func loadEmailConfig() *email.Client {
	address := os.Getenv("APP_SMTP_EMAIL")
	password := os.Getenv("APP_SMTP_PASSWORD")
	host := os.Getenv("APP_SMTP_HOST")
	port := os.Getenv("APP_SMTP_PORT")

	if address == "" || password == "" || host == "" {
		log.Println("Missing email configuration.")
		return nil
	}

	if port == "" {
		port = "587"
	}

	return email.NewClient(address, password, host, port)
}

func sendEmail(mail email.Email) error {
	if m == nil {
		return errors.New("email client is not initialized")
	}
	return m.Send(mail)
}

func sendResetEmail(address, token string) {
	resetURL := fmt.Sprintf("%s/reset-password-confirm?token=%s", baseUrl, token)
	err := sendEmail(email.Email{
		To:      []string{address},
		Subject: "Reset password",
		Body:    fmt.Sprintf("Use the following link to reset your password:\n%s", resetURL),
	})
	if err != nil {
		log.Printf("Could not send reset email for %s. Link: %s", address, resetURL)
	}
}

func readSessionCookie(r *http.Request) (userID *uint, err error) {
	cookie, err := r.Cookie("session_token")
	if err != nil {
		return
	}
	return ks.Get("session:" + cookie.Value)
}

// Middleware to check if the user is logged in
func loginRequired(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID, err := readSessionCookie(r)
		if err != nil {
			http.Redirect(w, r, "/login", http.StatusFound)
			return
		}

		ctx := context.WithValue(r.Context(), userContextKey, *userID)
		next(w, r.WithContext(ctx))
	}
}

func getLoggedUser(r *http.Request) (user User, ok bool) {
	userID, ok := r.Context().Value(userContextKey).(uint)
	if !ok {
		return
	}

	if db.Find(&user, userID).Error != nil {
		ok = true
	}

	return user, ok
}

func formatDuration(t time.Time) string {
	days := int(time.Since(t).Hours()) / 24

	switch {
	case days == 0:
		return "Today"
	case days == 1:
		return "Yesterday"
	case days <= 7:
		return fmt.Sprintf("%d day(s) ago", days)
	case days <= 30:
		weeks := days / 7
		remainingDays := days % 7
		if remainingDays == 0 {
			return fmt.Sprintf("%d week(s) ago", weeks)
		}
		return fmt.Sprintf("%d wee(k), %d day(s) ago", weeks, remainingDays)
	default:
		months := days / 30
		remainingDays := days % 30
		if remainingDays == 0 {
			return fmt.Sprintf("%d month(s) ago", months)
		}
		return fmt.Sprintf("%d month(s), %d day(s) ago", months, remainingDays)
	}
}

func toHabitDisplay(habit Habit) HabitDisplay {
	var lastAck string
	if habit.LastAck == nil {
		lastAck = "-"
	} else {
		lastAck = formatDuration(*habit.LastAck)
	}

	return HabitDisplay{
		ID:       habit.ID,
		Name:     habit.Name,
		LastAck:  lastAck,
		Disabled: habit.Disabled,
		Class:    classGood,
	}
}

func getHabit(id uint) (habit Habit, err error) {
	err = db.Model(&Habit{}).Find(&habit, id).Error
	return
}

func getHabitHelper(w http.ResponseWriter, r *http.Request) (habit Habit, err error) {
	id := getID(r)
	if id == 0 {
		err = errors.New("no id")
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	user, ok := getLoggedUser(r)
	if !ok {
		err = errors.New("no logged user")
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	habit, err = getHabit(id)
	if err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}

	if habit.UserID != user.ID {
		err = errors.New("forbidden")
		http.Error(w, "forbidden", http.StatusForbidden)
	}
	return
}

func getAllHabits(userID uint) (positives []HabitDisplay, negatives []HabitDisplay, err error) {
	var habits []Habit
	err = db.Model(&Habit{}).Where(&Habit{UserID: userID}).Find(&habits).Error
	if err != nil {
		return
	}

	for _, habit := range habits {
		habitDisplay := toHabitDisplay(habit)
		if habit.Negative {
			negatives = append(negatives, habitDisplay)
		} else {
			positives = append(positives, habitDisplay)
		}
	}
	return
}

func getID(r *http.Request) uint {
	res, err := strconv.ParseUint(r.PathValue("id"), 10, 64)
	if err != nil {
		return 0
	}
	return uint(res)
}
