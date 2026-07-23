FROM nginx:alpine
COPY index.html /usr/share/nginx/html/index.html
# nginx:alpine listens on :80 and ships busybox wget for the platform's health
# check. SIGQUIT makes nginx drain connections gracefully on `docker stop`.
STOPSIGNAL SIGQUIT
EXPOSE 80
