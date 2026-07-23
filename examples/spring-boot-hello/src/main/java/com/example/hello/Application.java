package com.example.hello;

import org.springframework.boot.SpringApplication;
import org.springframework.boot.autoconfigure.SpringBootApplication;
import org.springframework.web.bind.annotation.GetMapping;
import org.springframework.web.bind.annotation.RestController;

// Minimal Spring Boot example for fast-infra. Actuator provides the health
// endpoint at /actuator/health; graceful shutdown is enabled in
// application.properties so in-flight requests finish on SIGTERM.
@SpringBootApplication
@RestController
public class Application {

    public static void main(String[] args) {
        SpringApplication.run(Application.class, args);
    }

    @GetMapping("/")
    public String hello() {
        return "hello from spring-boot-hello — deployed with fast-infra\n";
    }
}
