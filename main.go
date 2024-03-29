package main

import (
	"fmt"
	"net/http"
	"os"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/gorilla/csrf"
	"github.com/liuminhaw/wrestic-brw/controllers"
	"github.com/liuminhaw/wrestic-brw/models"
	"github.com/liuminhaw/wrestic-brw/restic"
	"github.com/liuminhaw/wrestic-brw/static"
	"github.com/liuminhaw/wrestic-brw/templates"
	"github.com/liuminhaw/wrestic-brw/utils/dotenv"
	"github.com/liuminhaw/wrestic-brw/utils/encryptor"
	"github.com/liuminhaw/wrestic-brw/views"
)

type config struct {
	PSQL models.PostgresConfig
	CSRF struct {
		Key    string
		Secure bool
	}
	Server struct {
		Address string // default localhost:3000
	}
	Encryption struct {
		Key [32]byte
	}
}

// loadEnvConfig loads config setting from .env file
func loadEnvConfig() (config, error) {
	var cfg config
	err := dotenv.LoadDotEnv()
	if err != nil {
		return cfg, err
	}

	// Read PostgreSQL values from env variables
	cfg.PSQL.Host = os.Getenv("DB_HOST")
	cfg.PSQL.Port = os.Getenv("DB_PORT")
	cfg.PSQL.User = os.Getenv("DB_USER")
	cfg.PSQL.Password = os.Getenv("DB_PASSWORD")
	cfg.PSQL.Database = os.Getenv("DB_DATABASE")
	cfg.PSQL.SSLMode = os.Getenv("DB_SSLMODE")

	// Read CSRF value
	csrf_secure := os.Getenv("CSRF_SECURE")
	csrf_secure_b, err := strconv.ParseBool(csrf_secure)
	if err != nil {
		return cfg, err
	}
	cfg.CSRF.Secure = csrf_secure_b
	cfg.CSRF.Key = os.Getenv("CSRF_KEY")

	// Read encryption key
	encKey := os.Getenv("ENC_KEY")
	encKeyBytes, err := encryptor.UrlDecodeKey(encKey)
	if err != nil {
		return cfg, fmt.Errorf("load env config: decode enc key: %w", err)
	}
	cfg.Encryption.Key = encKeyBytes

	// TODO: Read the server value from an ENV variable
	cfg.Server.Address = ":4000"

	return cfg, nil
}

func main() {
	// Check restic command
	if err := restic.ResticCheck(); err != nil {
		fmt.Printf("restic command not found: %s\n", err)
		os.Exit(1)
	}

	cfg, err := loadEnvConfig()
	if err != nil {
		panic(err)
	}

	// Setup the database
	fmt.Println(cfg.PSQL.String())
	db, err := models.Open(cfg.PSQL)
	if err != nil {
		panic(err)
	}
	defer db.Close()

	// Setup services
	userService := &models.UserService{
		DB: db,
	}
	sessionService := &models.SessionService{
		DB: db,
	}
	repositoryService := &restic.RepositoryService{
		DB:     db,
		EncKey: cfg.Encryption.Key,
	}
	repositoryStatusService := &restic.RepositoryStatusService{
		DB: db,
	}

	// Setup middleware
	umw := controllers.UserMiddleware{
		SessionService: sessionService,
	}

	csrfMw := csrf.Protect(
		[]byte(cfg.CSRF.Key),
		csrf.Secure(cfg.CSRF.Secure),
		csrf.Path("/"),
	)

	// Setup controllers
	usersC := controllers.Users{
		UserService:    userService,
		SessionService: sessionService,
	}
	usersC.Templates.SignIn = views.Must(views.ParseFS(
		templates.FS,
		"tailwind.gohtml", "default.gohtml", "signin.gohtml",
	))
	repositoriesC := controllers.Repositories{
		RepositoryService:       repositoryService,
		RepositoryStatusService: repositoryStatusService,
	}
	repositoriesC.Templates.Index = views.Must(views.ParseFS(
		templates.FS,
		"tailwind.gohtml", "repositories/index.html",
	))
	repositoriesC.Templates.New = views.Must(views.ParseFS(
		templates.FS,
		"tailwind.gohtml", "repositories/new.gohtml",
	))

	r := chi.NewRouter()
	r.Use(csrfMw)
	r.Use(umw.SetUser)

	fileServer := http.FileServer(http.FS(static.FS))
	r.Get("/static/*", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.StripPrefix("/static", fileServer).ServeHTTP(w, r)
	}))

	r.Get("/", usersC.SignIn)
	r.Post("/signin", usersC.ProcessSignIn)
	r.Post("/signout", usersC.ProcessSignOut)

	tpl := views.Must(
		views.ParseFS(templates.FS, "tailwind.gohtml", "default.gohtml", "home.gohtml"),
	)
	r.Route("/hello", func(r chi.Router) {
		r.Use(umw.RequireUser)
		r.Get("/", controllers.StaticHandler(tpl))
	})

	r.Route("/repositories", func(r chi.Router) {
		r.Use(umw.RequireUser)
		r.Get("/", repositoriesC.Index)
		r.Post("/", repositoriesC.Create)
		r.Get("/new", repositoriesC.New)
	})

	// r.Get("/signin", usersC.SignIn)

	// Start server
	fmt.Printf("Starting the server on %s...", cfg.Server.Address)
	// err = http.ListenAndServe(cfg.Server.Address, r)
	server := &http.Server{
		Addr:    cfg.Server.Address,
		Handler: r,
	}
	err = server.ListenAndServe()
}

// func TimerMiddleware(h http.HandlerFunc) http.HandlerFunc {
// 	return func(w http.ResponseWriter, r *http.Request) {
// 		start := time.Now()
// 		h(w, r)
// 		fmt.Println("Request time:", time.Since(start))
// 	}
// }
