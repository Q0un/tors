
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <unistd.h>
#include <arpa/inet.h>
#include <sys/socket.h>
#include <errno.h>
#include <pthread.h>
#include <netinet/tcp.h>

#define UDP_PORT 8000
#define TCP_PORT 8001
#define BUFFER_SIZE 1024
#define INTERVALS 10000
#define MAX_TASKS 10

double f(double x) {
    return x * x;
}

typedef struct Task {
    int id;
    double l;
    double r;
} Task;

Task* tasks[MAX_TASKS];
int n_tasks = 0;
pthread_mutex_t m;
int client_sock = -1;

void set_tcp_keepalive(int sockfd, int keep_idle, int keep_interval, int keep_count) {
    int optval = 1;

    // Включить TCP Keepalive
    if (setsockopt(sockfd, SOL_SOCKET, SO_KEEPALIVE, &optval, sizeof(optval)) < 0) {
        perror("setsockopt(SO_KEEPALIVE) failed");
        exit(EXIT_FAILURE);
    }

    // Установить временной промежуток, через который будут начинаться Keepalive пакеты
    if (setsockopt(sockfd, IPPROTO_TCP, TCP_KEEPIDLE, &keep_idle, sizeof(keep_idle)) < 0) {
        perror("setsockopt(TCP_KEEPIDLE) failed");
        exit(EXIT_FAILURE);
    }

    // Установить интервал между Keepalive пакетами
    if (setsockopt(sockfd, IPPROTO_TCP, TCP_KEEPINTVL, &keep_interval, sizeof(keep_interval)) < 0) {
        perror("setsockopt(TCP_KEEPINTVL) failed");
        exit(EXIT_FAILURE);
    }

    // Установить количество Keepalive пакетов до закрытия соединения
    if (setsockopt(sockfd, IPPROTO_TCP, TCP_KEEPCNT, &keep_count, sizeof(keep_count)) < 0) {
        perror("setsockopt(TCP_KEEPCNT) failed");
        exit(EXIT_FAILURE);
    }
}

void* tasks_thread_func(void* vargp) {
    while (1) {
        pthread_mutex_lock(&m);
        Task* task = tasks[0];
        printf("Trying... %ld\n", (long)task);
        fflush(stdout);
        if (task == NULL) {
            pthread_mutex_unlock(&m);
            sleep(1);
            continue;
        }
        for (int i = 1; i < n_tasks; ++i) {
            tasks[i - 1] = tasks[i];
        }
        tasks[--n_tasks] = NULL;
        pthread_mutex_unlock(&m);

        double delta = (task->r - task->l) / INTERVALS;
        double res = 0;

        for (int i = 0; i < INTERVALS; ++i) {
            double x = task->l + i * delta;
            res += delta * f(x);
        }

        pthread_mutex_lock(&m);
        printf("RES: %lf\n", res);
        fflush(stdout);
        char response[BUFFER_SIZE];
        sprintf(response, "%d %lf", task->id, res);
        if (send(client_sock, response, strlen(response), 0) < 0) {
            close(client_sock);
            client_sock = -1;
            printf("BAD RES\n");
            fflush(stdout);
        }
        printf("SENDED\n");
        fflush(stdout);
        pthread_mutex_unlock(&m);
        free(task);
    }
}

void handle_udp_requests(int udp_sock) {
    struct sockaddr_in client_addr;
    socklen_t addr_len = sizeof(client_addr);
    char buffer[BUFFER_SIZE];

    while (1) {
        int recv_len = recvfrom(udp_sock, buffer, BUFFER_SIZE - 1, 0,
                                (struct sockaddr*)&client_addr, &addr_len);
        if (recv_len < 0) {
            perror("recvfrom");
            continue;
        }

        buffer[recv_len] = '\0';
        printf("Received broadcast message: %s\n", buffer);

        // Ответим клиенту
        const char *response = "ACK";
        if (sendto(udp_sock, response, strlen(response), 0,
                   (struct sockaddr*)&client_addr, addr_len) < 0) {
            perror("sendto");
        } else {
            break;
        }
    }
}

void handle_tcp_connection(int tcp_sock) {
    while (1) {
        struct sockaddr_in client_addr;
        socklen_t addr_len = sizeof(client_addr);

        pthread_mutex_lock(&m);
        client_sock = accept(tcp_sock, (struct sockaddr*)&client_addr, &addr_len);
        if (client_sock < 0) {
            perror("accept");
            pthread_mutex_unlock(&m);
            continue;
        }
        pthread_mutex_unlock(&m);

        set_tcp_keepalive(client_sock, 2, 1, 3);

        printf("Accepted TCP connection from %s:%d\n",
               inet_ntoa(client_addr.sin_addr), ntohs(client_addr.sin_port));

        // Общение с клиентом
        while (1) {
            char buffer[BUFFER_SIZE];
            printf("Reading...\n");
            fflush(stdout);

            fd_set read_fds;
            FD_ZERO(&read_fds);
            FD_SET(client_sock, &read_fds);

            struct timeval timeout;
            timeout.tv_sec = 1;
            timeout.tv_usec = 0;

            int activity = select(client_sock + 1, &read_fds, NULL, NULL, &timeout);
            if (activity < 0) {
                perror("select");
                break;
            } else if (activity == 0 || !FD_ISSET(client_sock, &read_fds)) {
                continue;
            }

            int recv_len = recv(client_sock, buffer, BUFFER_SIZE - 1, 0);
            if (recv_len < 0) {
                break;
            } else if (recv_len == 0) {
                break;
            }
            buffer[recv_len] = '\0';
            printf("%s\n", buffer);
            fflush(stdout);

            Task* task = malloc(sizeof(Task));
            sscanf(buffer, "%d %lf %lf", &task->id, &task->l, &task->r);

            pthread_mutex_lock(&m);
            printf("Add task\n");
            fflush(stdout);
            tasks[n_tasks++] = task;
            pthread_mutex_unlock(&m);
        }
        pthread_mutex_lock(&m);
        close(client_sock);
        client_sock = -1;
        pthread_mutex_unlock(&m);
    }
}

int main(int argc, char* argv[]) {
    pthread_t tasks_thread;
    pthread_mutex_init(&m, NULL); //  инициализация мьютекса
    pthread_create(&tasks_thread, NULL, tasks_thread_func, NULL);

    // Создаем UDP-сокет для приема и ответа на широковещательное сообщение
    int udp_sock = socket(AF_INET, SOCK_DGRAM, 0);
    if (udp_sock < 0) {
        perror("socket");
        exit(EXIT_FAILURE);
    }

    int one = 1;

    if (setsockopt(udp_sock, SOL_SOCKET, SO_REUSEADDR, &one, sizeof(one)) < 0) {
        perror("setsockopt failed");
        close(udp_sock);
        exit(EXIT_FAILURE);
    }

    if (setsockopt(udp_sock, SOL_SOCKET, SO_REUSEPORT, &one, sizeof(one)) < 0) {
        perror("setsockopt failed");
        close(udp_sock);
        exit(EXIT_FAILURE);
    }

    struct sockaddr_in server_addr;
    memset(&server_addr, 0, sizeof(server_addr));
    server_addr.sin_family = AF_INET;
    server_addr.sin_addr.s_addr = inet_addr(argv[1]);
    server_addr.sin_port = htons(UDP_PORT);

    if (bind(udp_sock, (struct sockaddr*)&server_addr, sizeof(server_addr)) < 0) {
        perror("bind");
        close(udp_sock);
        exit(EXIT_FAILURE);
    }

    // Создаем TCP-сокет для приема входящих соединений
    int tcp_sock = socket(AF_INET, SOCK_STREAM, 0);
    if (tcp_sock < 0) {
        perror("socket");
        close(udp_sock);
        exit(EXIT_FAILURE);
    }

    if (setsockopt(tcp_sock, SOL_SOCKET, SO_REUSEADDR, &one, sizeof(one)) < 0) {
        perror("setsockopt failed");
        close(tcp_sock);
        exit(EXIT_FAILURE);
    }

    if (setsockopt(tcp_sock, SOL_SOCKET, SO_REUSEPORT, &one, sizeof(one)) < 0) {
        perror("setsockopt failed");
        close(tcp_sock);
        exit(EXIT_FAILURE);
    }

    server_addr.sin_port = htons(TCP_PORT);

    if (bind(tcp_sock, (struct sockaddr*)&server_addr, sizeof(server_addr)) < 0) {
        perror("bind");
        close(udp_sock);
        close(tcp_sock);
        exit(EXIT_FAILURE);
    }

    if (listen(tcp_sock, 10) < 0) {
        perror("listen");
        close(udp_sock);
        close(tcp_sock);
        exit(EXIT_FAILURE);
    }

    handle_udp_requests(udp_sock);
    handle_tcp_connection(tcp_sock);

    close(udp_sock);
    close(tcp_sock);

    return 0;
}
