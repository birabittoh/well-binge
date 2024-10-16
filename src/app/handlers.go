package app

import (
	"net/http"
	"strconv"
	"time"
)

func getIndexHandler(w http.ResponseWriter, r *http.Request) {
	xt.ExecuteTemplate(w, "index.tmpl", nil)
}

func getHabitsHandler(w http.ResponseWriter, r *http.Request) {
	user, ok := getLoggedUser(r)
	if !ok {
		http.Error(w, "Could not find user in context.", http.StatusInternalServerError)
		return
	}

	positive, negative, err := getAllHabits(user.ID)
	if err != nil {
		http.Error(w, "Could not get user habits.", http.StatusInternalServerError)
		return
	}

	data := map[string]interface{}{
		"User":     user,
		"Positive": positive,
		"Negative": negative,
	}

	xt.ExecuteTemplate(w, "habits.tmpl", data)
}

func getHabitsIDHandler(w http.ResponseWriter, r *http.Request) {
	habit, err := getHabitHelper(w, r)
	if err != nil {
		return
	}

	xt.ExecuteTemplate(w, "habits-id.tmpl", habit)
}

func getNewPositiveHandler(w http.ResponseWriter, r *http.Request) {
	data := map[string]interface{}{"Negative": false}
	xt.ExecuteTemplate(w, "new.tmpl", data)
}

func getNewNegativeHandler(w http.ResponseWriter, r *http.Request) {
	data := map[string]interface{}{"Negative": true}
	xt.ExecuteTemplate(w, "new.tmpl", data)
}

func postNewHandler(w http.ResponseWriter, r *http.Request) {
	negative := r.FormValue("negative") == "on"
	name := r.FormValue("name")

	if !checkHabitName(name) {
		http.Error(w, "Bad habit name.", http.StatusBadRequest)
		return
	}

	var days uint
	if !negative {
		res, err := strconv.ParseUint(r.FormValue("days"), 10, 64)
		if err != nil {
			http.Error(w, "Bad days value.", http.StatusBadRequest)
			return
		}
		days = uint(res)
	}

	user, ok := getLoggedUser(r)
	if !ok {
		http.Error(w, "Could not get logged user", http.StatusInternalServerError)
		return
	}

	db.Create(&Habit{
		UserID:   user.ID,
		Name:     name,
		Days:     days,
		Negative: negative,
	})

	http.Redirect(w, r, "/habits", http.StatusFound)
}

func postHabitsIDHandler(w http.ResponseWriter, r *http.Request) {
	habit, err := getHabitHelper(w, r)
	if err != nil {
		return
	}

	var changed bool

	name := r.FormValue("name")
	if name != habit.Name {
		if !checkHabitName(name) {
			http.Error(w, "Bad habit name.", http.StatusBadRequest)
			return
		}
		habit.Name = name
		changed = true
	}

	if !habit.Negative {
		res, err := strconv.ParseUint(r.FormValue("days"), 10, 64)
		if err != nil {
			http.Error(w, "Bad days value.", http.StatusBadRequest)
			return
		}
		days := uint(res)
		if days != habit.Days {
			habit.Days = days
			changed = true
		}
	}

	disabled := r.FormValue("enabled") != "on"
	if disabled != habit.Disabled {
		habit.Disabled = disabled
		changed = true
	}

	if changed {
		db.Save(&habit)
	}

	http.Redirect(w, r, "/habits", http.StatusFound)
}

func postDeleteIDHandler(w http.ResponseWriter, r *http.Request) {
	id := getID(r)
	if id == 0 {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	user, ok := getLoggedUser(r)
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	db.Delete(&Habit{}, "id = ? AND user_id = ?", id, user.ID)

	http.Redirect(w, r, "/habits", http.StatusFound)
}

func postAckIDHandler(w http.ResponseWriter, r *http.Request) {
	habit, err := getHabitHelper(w, r)
	if err != nil {
		return
	}

	if habit.LastAck != nil {
		if time.Since(*habit.LastAck) < 6*time.Hour {
			http.Redirect(w, r, "/habits", http.StatusFound) // TODO: redirect to an error page instead
			return
		}
	}

	db.Create(&Ack{HabitID: habit.ID})

	now := time.Now()
	habit.LastAck = &now
	db.Save(&habit)

	http.Redirect(w, r, "/habits", http.StatusFound)
}

func getRegisterHandler(w http.ResponseWriter, r *http.Request) {
	xt.ExecuteTemplate(w, "auth-register.tmpl", nil)
}

func getLoginHandler(w http.ResponseWriter, r *http.Request) {
	_, err := readSessionCookie(r)
	if err != nil {
		xt.ExecuteTemplate(w, "auth-login.tmpl", nil)
		return
	}

	http.Redirect(w, r, "/habits", http.StatusFound)
}

func getResetPasswordHandler(w http.ResponseWriter, r *http.Request) {
	xt.ExecuteTemplate(w, "auth-reset_password.tmpl", nil)
}

func postRegisterHandler(w http.ResponseWriter, r *http.Request) {
	if !registrationEnabled {
		http.Error(w, "Registration is currently disabled.", http.StatusForbidden)
		return
	}

	username, err := sanitizeUsername(r.FormValue("username"))
	if err != nil {
		http.Error(w, "Invalid username.", http.StatusBadRequest)
		return
	}

	email, err := sanitizeEmail(r.FormValue("email"))
	if err != nil {
		http.Error(w, "Invalid email.", http.StatusBadRequest)
		return
	}

	_, err = getUserByName(username, 0)
	if err == nil {
		http.Error(w, "This username is already registered.", http.StatusConflict)
		return
	}

	hashedPassword, salt, err := g.HashPassword(r.FormValue("password"))
	if err != nil {
		http.Error(w, "Invalid password.", http.StatusBadRequest)
		return
	}

	user := User{
		Username:     username,
		Email:        email,
		PasswordHash: hashedPassword,
		Salt:         salt,
	}

	db.Create(&user)
	if user.ID == 0 {
		http.Error(w, "This email is already registered.", http.StatusConflict)
		return
	}

	login(w, user.ID, false)
	http.Redirect(w, r, "/login", http.StatusFound)
}

func postLoginHandler(w http.ResponseWriter, r *http.Request) {
	username := r.FormValue("username")
	password := r.FormValue("password")
	remember := r.FormValue("remember")

	user, err := getUserByName(username, 0)

	if err != nil || !g.CheckPassword(password, user.Salt, user.PasswordHash) {
		http.Error(w, "Invalid credentials", http.StatusUnauthorized)
		return
	}

	login(w, user.ID, remember == "on")
	http.Redirect(w, r, "/login", http.StatusFound)
}

func logoutHandler(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, g.GenerateEmptyCookie())
	http.Redirect(w, r, "/login", http.StatusFound)
}

func postResetPasswordHandler(w http.ResponseWriter, r *http.Request) {
	emailInput := r.FormValue("email")

	var user User
	db.Where("email = ?", emailInput).First(&user)

	if user.ID == 0 {
		http.Redirect(w, r, "/login", http.StatusFound)
		return
	}

	resetToken, err := g.GenerateRandomToken(32)
	if err != nil {
		http.Error(w, "Could not generate reset token.", http.StatusInternalServerError)
		return
	}

	ks.Set("reset:"+resetToken, user.ID, time.Hour)
	sendResetEmail(user.Email, resetToken)

	http.Redirect(w, r, "/login", http.StatusFound)

}

func getResetPasswordConfirmHandler(w http.ResponseWriter, r *http.Request) {
	token := r.URL.Query().Get("token")
	_, err := ks.Get("reset:" + token)
	if err != nil {
		http.Error(w, "Token is invalid or expired.", http.StatusUnauthorized)
		return
	}

	xt.ExecuteTemplate(w, "auth-new_password.tmpl", nil)
}

func postResetPasswordConfirmHandler(w http.ResponseWriter, r *http.Request) {
	token := r.URL.Query().Get("token")
	userID, err := ks.Get("reset:" + token)
	if err != nil {
		http.Error(w, "Token is invalid or expired.", http.StatusUnauthorized)
		return
	}

	var user User
	db.First(&user, *userID)

	password := r.FormValue("password")

	hashedPassword, salt, err := g.HashPassword(password)
	if err != nil {
		http.Error(w, "Invalid password.", http.StatusBadRequest)
		return
	}

	user.PasswordHash = hashedPassword
	user.Salt = salt
	db.Save(&user)
	ks.Delete(token)

	http.Redirect(w, r, "/login", http.StatusFound)
}
